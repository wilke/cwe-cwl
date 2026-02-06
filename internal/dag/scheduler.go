package dag

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Scheduler manages the execution of DAG nodes.
type Scheduler struct {
	dag         *DAG
	executor    Executor
	maxParallel int
	pollInterval time.Duration
	mu          sync.Mutex
	running     map[string]bool
	ctx         context.Context
	cancel      context.CancelFunc
}

// Executor is the interface for executing DAG nodes.
type Executor interface {
	Execute(ctx context.Context, node *Node) error
	GetStatus(ctx context.Context, taskID string) (NodeStatus, error)
	GetOutputs(ctx context.Context, taskID string) (map[string]interface{}, error)
	Cancel(ctx context.Context, taskID string) error
}

// NewScheduler creates a new scheduler.
func NewScheduler(dag *DAG, executor Executor, maxParallel int) *Scheduler {
	return &Scheduler{
		dag:          dag,
		executor:     executor,
		maxParallel:  maxParallel,
		pollInterval: 5 * time.Second,
		running:      make(map[string]bool),
	}
}

// SetPollInterval sets the interval for checking task status.
func (s *Scheduler) SetPollInterval(interval time.Duration) {
	s.pollInterval = interval
}

// Run executes the DAG until completion or failure.
func (s *Scheduler) Run(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	defer s.cancel()

	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		default:
		}

		// Check if DAG is complete
		if s.dag.IsComplete() {
			if s.dag.HasFailed() {
				return fmt.Errorf("workflow failed")
			}
			return nil
		}

		// Update status of running nodes
		if err := s.updateRunningNodes(); err != nil {
			return err
		}

		// Schedule ready nodes
		if err := s.scheduleReadyNodes(); err != nil {
			return err
		}

		// Wait before next iteration
		time.Sleep(s.pollInterval)
	}
}

// scheduleReadyNodes schedules ready nodes for execution.
func (s *Scheduler) scheduleReadyNodes() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	readyNodes := s.dag.GetReadyNodes()

	for _, node := range readyNodes {
		// Check if we've reached max parallelism
		if s.maxParallel > 0 && len(s.running) >= s.maxParallel {
			break
		}

		// Skip if already running
		if s.running[node.ID] {
			continue
		}

		// Execute the node
		if err := s.executeNode(node); err != nil {
			node.SetError(err.Error())
			if err := s.dag.UpdateNodeStatus(node.ID, StatusFailed); err != nil {
				return err
			}
			continue
		}

		s.running[node.ID] = true
		if err := s.dag.UpdateNodeStatus(node.ID, StatusRunning); err != nil {
			return err
		}
	}

	return nil
}

// executeNode starts execution of a node.
func (s *Scheduler) executeNode(node *Node) error {
	return s.executor.Execute(s.ctx, node)
}

// updateRunningNodes checks status of running nodes.
func (s *Scheduler) updateRunningNodes() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for nodeID := range s.running {
		node := s.dag.GetNode(nodeID)
		if node == nil {
			delete(s.running, nodeID)
			continue
		}

		taskID := node.GetTaskID()
		if taskID == "" {
			continue
		}

		status, err := s.executor.GetStatus(s.ctx, taskID)
		if err != nil {
			continue // Transient error, will retry
		}

		switch status {
		case StatusCompleted:
			// Fetch outputs
			outputs, err := s.executor.GetOutputs(s.ctx, taskID)
			if err != nil {
				node.SetError(fmt.Sprintf("failed to get outputs: %v", err))
				if err := s.dag.UpdateNodeStatus(nodeID, StatusFailed); err != nil {
					return err
				}
			} else {
				node.SetOutputs(outputs)
				if err := s.dag.UpdateNodeStatus(nodeID, StatusCompleted); err != nil {
					return err
				}
			}
			delete(s.running, nodeID)

		case StatusFailed:
			if err := s.dag.UpdateNodeStatus(nodeID, StatusFailed); err != nil {
				return err
			}
			delete(s.running, nodeID)

		case StatusRunning:
			// Still running, continue monitoring
		}
	}

	return nil
}

// Cancel cancels the scheduler and all running tasks.
func (s *Scheduler) Cancel() error {
	if s.cancel != nil {
		s.cancel()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for nodeID := range s.running {
		node := s.dag.GetNode(nodeID)
		if node == nil {
			continue
		}
		taskID := node.GetTaskID()
		if taskID != "" {
			if err := s.executor.Cancel(s.ctx, taskID); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors cancelling tasks: %v", errs)
	}
	return nil
}

// GetProgress returns the current execution progress.
func (s *Scheduler) GetProgress() Progress {
	stats := s.dag.GetStats()
	total := len(s.dag.Nodes)

	return Progress{
		Total:     total,
		Pending:   stats[StatusPending],
		Ready:     stats[StatusReady],
		Running:   stats[StatusRunning],
		Completed: stats[StatusCompleted],
		Failed:    stats[StatusFailed],
		Skipped:   stats[StatusSkipped],
	}
}

// Progress represents workflow execution progress.
type Progress struct {
	Total     int
	Pending   int
	Ready     int
	Running   int
	Completed int
	Failed    int
	Skipped   int
}

// PercentComplete returns the completion percentage.
func (p Progress) PercentComplete() float64 {
	if p.Total == 0 {
		return 100.0
	}
	done := p.Completed + p.Failed + p.Skipped
	return float64(done) / float64(p.Total) * 100
}

// IsComplete returns true if all tasks are finished.
func (p Progress) IsComplete() bool {
	return p.Pending == 0 && p.Ready == 0 && p.Running == 0
}

// EventType represents the type of scheduler event.
type EventType string

const (
	EventNodeStarted   EventType = "node_started"
	EventNodeCompleted EventType = "node_completed"
	EventNodeFailed    EventType = "node_failed"
	EventNodeSkipped   EventType = "node_skipped"
	EventWorkflowDone  EventType = "workflow_done"
)

// Event represents a scheduler event.
type Event struct {
	Type     EventType
	NodeID   string
	StepID   string
	TaskID   string
	Error    string
	Outputs  map[string]interface{}
	Progress Progress
}

// EventHandler handles scheduler events.
type EventHandler func(Event)

// SchedulerWithEvents extends Scheduler with event handling.
type SchedulerWithEvents struct {
	*Scheduler
	handlers []EventHandler
	mu       sync.RWMutex
}

// NewSchedulerWithEvents creates a scheduler with event handling.
func NewSchedulerWithEvents(dag *DAG, executor Executor, maxParallel int) *SchedulerWithEvents {
	return &SchedulerWithEvents{
		Scheduler: NewScheduler(dag, executor, maxParallel),
		handlers:  make([]EventHandler, 0),
	}
}

// OnEvent registers an event handler.
func (s *SchedulerWithEvents) OnEvent(handler EventHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, handler)
}

// emit sends an event to all handlers.
func (s *SchedulerWithEvents) emit(event Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	event.Progress = s.GetProgress()
	for _, handler := range s.handlers {
		handler(event)
	}
}
