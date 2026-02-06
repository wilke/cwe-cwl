// Package events provides Redis pub/sub event handling.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/state"
)

// Event types
const (
	TaskCompletionChannel = "task_completion"
	TaskSubmissionChannel = "task_submission"
	WorkflowEventChannel  = "workflow_events"
)

// TaskCompletionEvent represents a task completion event from BV-BRC.
type TaskCompletionEvent struct {
	Type      string `json:"type"`
	TaskID    int64  `json:"task_id"`
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp int64  `json:"time"`
}

// WorkflowEvent represents a workflow-level event.
type WorkflowEvent struct {
	Type         string `json:"type"`
	WorkflowID   string `json:"workflow_id"`
	WorkflowName string `json:"workflow_name,omitempty"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	Timestamp    int64  `json:"time"`
}

// EventHandler handles incoming events.
type EventHandler interface {
	HandleTaskCompletion(ctx context.Context, event TaskCompletionEvent) error
}

// Subscriber subscribes to Redis channels and processes events.
type Subscriber struct {
	config   *config.Config
	redis    *redis.Client
	store    *state.Store
	handlers []EventHandler
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewSubscriber creates a new event subscriber.
func NewSubscriber(cfg *config.Config, redisClient *redis.Client, store *state.Store) *Subscriber {
	return &Subscriber{
		config:   cfg,
		redis:    redisClient,
		store:    store,
		handlers: make([]EventHandler, 0),
	}
}

// AddHandler adds an event handler.
func (s *Subscriber) AddHandler(handler EventHandler) {
	s.handlers = append(s.handlers, handler)
}

// Start begins listening for events.
func (s *Subscriber) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Subscribe to task completion channel
	pubsub := s.redis.Subscribe(s.ctx, TaskCompletionChannel)
	defer pubsub.Close()

	// Wait for subscription confirmation
	if _, err := pubsub.Receive(s.ctx); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	log.Printf("Subscribed to %s channel", TaskCompletionChannel)

	// Process messages
	ch := pubsub.Channel()
	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case msg := <-ch:
			if msg == nil {
				continue
			}
			if err := s.processMessage(msg); err != nil {
				log.Printf("Error processing message: %v", err)
			}
		}
	}
}

// Stop stops the subscriber.
func (s *Subscriber) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// processMessage processes a Redis message.
func (s *Subscriber) processMessage(msg *redis.Message) error {
	var event TaskCompletionEvent
	if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	// Call all handlers
	for _, handler := range s.handlers {
		if err := handler.HandleTaskCompletion(s.ctx, event); err != nil {
			log.Printf("Handler error: %v", err)
		}
	}

	return nil
}

// Publisher publishes events to Redis.
type Publisher struct {
	redis *redis.Client
}

// NewPublisher creates a new event publisher.
func NewPublisher(redisClient *redis.Client) *Publisher {
	return &Publisher{
		redis: redisClient,
	}
}

// PublishTaskCompletion publishes a task completion event.
func (p *Publisher) PublishTaskCompletion(ctx context.Context, event TaskCompletionEvent) error {
	event.Timestamp = time.Now().Unix()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.redis.Publish(ctx, TaskCompletionChannel, string(data)).Err()
}

// PublishWorkflowEvent publishes a workflow event.
func (p *Publisher) PublishWorkflowEvent(ctx context.Context, event WorkflowEvent) error {
	event.Timestamp = time.Now().Unix()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.redis.Publish(ctx, WorkflowEventChannel, string(data)).Err()
}

// WorkflowEventHandler handles workflow-related events.
type WorkflowEventHandler struct {
	store     *state.Store
	publisher *Publisher
}

// NewWorkflowEventHandler creates a new workflow event handler.
func NewWorkflowEventHandler(store *state.Store, publisher *Publisher) *WorkflowEventHandler {
	return &WorkflowEventHandler{
		store:     store,
		publisher: publisher,
	}
}

// HandleTaskCompletion handles task completion events.
func (h *WorkflowEventHandler) HandleTaskCompletion(ctx context.Context, event TaskCompletionEvent) error {
	// Look up step execution by BV-BRC task ID
	exec, err := h.store.GetStepExecutionByTaskID(ctx, event.TaskID)
	if err != nil {
		return fmt.Errorf("failed to get step execution: %w", err)
	}
	if exec == nil {
		// Not a CWL step, ignore
		return nil
	}

	// Update step execution status
	var stepStatus state.StepStatus
	switch event.Status {
	case "completed", "C":
		stepStatus = state.StepCompleted
	case "failed", "F", "E":
		stepStatus = state.StepFailed
	default:
		return nil // Unknown status, ignore
	}

	update := &state.StepExecutionUpdate{
		Status:       stepStatus,
		SetCompleted: true,
	}
	if event.Error != "" {
		update.ErrorMessage = event.Error
	}

	if err := h.store.UpdateStepExecution(ctx, exec.ID, update); err != nil {
		return fmt.Errorf("failed to update step execution: %w", err)
	}

	// Check if workflow is complete
	if err := h.checkWorkflowCompletion(ctx, exec.WorkflowRunID); err != nil {
		log.Printf("Error checking workflow completion: %v", err)
	}

	return nil
}

// checkWorkflowCompletion checks if a workflow has completed.
func (h *WorkflowEventHandler) checkWorkflowCompletion(ctx context.Context, workflowRunID string) error {
	progress, err := h.store.GetRunProgress(ctx, workflowRunID)
	if err != nil {
		return err
	}

	// Check if all steps are done
	if progress.Pending == 0 && progress.Running == 0 {
		var status state.WorkflowStatus
		if progress.Failed > 0 {
			status = state.WorkflowFailed
		} else {
			status = state.WorkflowCompleted
		}

		if err := h.store.UpdateWorkflowRunStatus(ctx, workflowRunID, status); err != nil {
			return err
		}

		// Publish workflow completion event
		run, err := h.store.GetWorkflowRun(ctx, workflowRunID)
		if err != nil {
			return err
		}
		if run != nil {
			h.publisher.PublishWorkflowEvent(ctx, WorkflowEvent{
				Type:       "workflow_completed",
				WorkflowID: run.ID,
				Status:     string(status),
			})
		}
	}

	return nil
}

// Poller polls for task status updates (backup for Redis pub/sub).
type Poller struct {
	config      *config.Config
	store       *state.Store
	checkStatus func(ctx context.Context, taskID int64) (string, error)
	interval    time.Duration
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewPoller creates a new task status poller.
func NewPoller(cfg *config.Config, store *state.Store, checkFunc func(ctx context.Context, taskID int64) (string, error)) *Poller {
	return &Poller{
		config:      cfg,
		store:       store,
		checkStatus: checkFunc,
		interval:    cfg.Executor.PollInterval,
	}
}

// Start begins polling for task status.
func (p *Poller) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-ticker.C:
			if err := p.pollRunningTasks(); err != nil {
				log.Printf("Error polling tasks: %v", err)
			}
		}
	}
}

// Stop stops the poller.
func (p *Poller) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// pollRunningTasks checks status of all running tasks.
func (p *Poller) pollRunningTasks() error {
	// Get all running workflow runs
	filter := state.WorkflowRunFilter{
		Status: string(state.WorkflowRunning),
		Limit:  100,
	}

	runs, err := p.store.ListWorkflowRuns(p.ctx, filter)
	if err != nil {
		return err
	}

	for _, run := range runs {
		steps, err := p.store.ListStepExecutions(p.ctx, run.ID)
		if err != nil {
			continue
		}

		for _, step := range steps {
			if step.Status == state.StepRunning && step.BVBRCTaskID != 0 {
				status, err := p.checkStatus(p.ctx, step.BVBRCTaskID)
				if err != nil {
					continue
				}

				// If status changed, trigger update
				if status == "C" || status == "F" || status == "E" {
					// This would trigger the same handler as the pub/sub
					log.Printf("Task %d status changed to %s", step.BVBRCTaskID, status)
				}
			}
		}
	}

	return nil
}

// ConnectRedis creates a Redis client from config.
func ConnectRedis(cfg *config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return client, nil
}

// ParseTaskID parses a task ID string to int64.
func ParseTaskID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
