// Package main provides the CWL scheduler daemon entry point.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver for BV-BRC database

	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/cwl"
	"github.com/BV-BRC/cwe-cwl/internal/dag"
	"github.com/BV-BRC/cwe-cwl/internal/events"
	"github.com/BV-BRC/cwe-cwl/internal/executor"
	"github.com/BV-BRC/cwe-cwl/internal/state"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Connect to MongoDB
	store, err := state.NewStore(cfg.MongoDB.URI, cfg.MongoDB.Database)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		store.Close(ctx)
	}()

	// Connect to Redis
	redisClient, err := events.ConnectRedis(&cfg.Redis)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisClient.Close()

	// Connect to BV-BRC database (PostgreSQL)
	var db *sql.DB
	var exec dag.Executor

	if cfg.Executor.Mode == "bvbrc" {
		db, err = sql.Open("postgres", cfg.BVBRC.DatabaseDSN)
		if err != nil {
			log.Fatalf("Failed to connect to BV-BRC database: %v", err)
		}
		defer db.Close()

		exec = executor.NewBVBRCExecutor(cfg, db, redisClient)
	} else {
		// Local executor for development
		exec = executor.NewLocalExecutor("/tmp/cwe-cwl-work")
	}

	// Create event publisher
	publisher := events.NewPublisher(redisClient)

	// Create event handler
	eventHandler := events.NewWorkflowEventHandler(store, publisher)

	// Create event subscriber
	subscriber := events.NewSubscriber(cfg, redisClient, store)
	subscriber.AddHandler(eventHandler)

	// Create scheduler instance
	schedulerRunner := &SchedulerRunner{
		config:    cfg,
		store:     store,
		executor:  exec,
		publisher: publisher,
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start event subscriber in background
	go func() {
		if err := subscriber.Start(ctx); err != nil && err != context.Canceled {
			log.Printf("Event subscriber error: %v", err)
		}
	}()

	// Start scheduler loop
	go func() {
		schedulerRunner.Run(ctx)
	}()

	log.Println("CWL Scheduler started")

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down scheduler...")
	cancel()
	subscriber.Stop()

	log.Println("Scheduler stopped")
}

// SchedulerRunner manages workflow execution.
type SchedulerRunner struct {
	config    *config.Config
	store     *state.Store
	executor  dag.Executor
	publisher *events.Publisher
}

// Run starts the scheduler loop.
func (sr *SchedulerRunner) Run(ctx context.Context) {
	ticker := time.NewTicker(sr.config.Executor.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sr.processWorkflows(ctx)
		}
	}
}

// processWorkflows processes pending and running workflows.
func (sr *SchedulerRunner) processWorkflows(ctx context.Context) {
	// Get pending workflows
	pending, err := sr.store.ListWorkflowRuns(ctx, state.WorkflowRunFilter{
		Status: string(state.WorkflowPending),
		Limit:  10,
	})
	if err != nil {
		log.Printf("Error listing pending workflows: %v", err)
		return
	}

	// Start pending workflows
	for _, run := range pending {
		if err := sr.startWorkflow(ctx, run.ID); err != nil {
			log.Printf("Error starting workflow %s: %v", run.ID, err)
		}
	}

	// Get running workflows
	running, err := sr.store.ListWorkflowRuns(ctx, state.WorkflowRunFilter{
		Status: string(state.WorkflowRunning),
		Limit:  50,
	})
	if err != nil {
		log.Printf("Error listing running workflows: %v", err)
		return
	}

	// Process running workflows
	for _, run := range running {
		if err := sr.processRunningWorkflow(ctx, run.ID); err != nil {
			log.Printf("Error processing workflow %s: %v", run.ID, err)
		}
	}
}

// startWorkflow starts a pending workflow.
func (sr *SchedulerRunner) startWorkflow(ctx context.Context, runID string) error {
	run, err := sr.store.GetWorkflowRun(ctx, runID)
	if err != nil || run == nil {
		return err
	}

	// Get the workflow document
	workflow, err := sr.store.GetWorkflow(ctx, run.WorkflowID)
	if err != nil || workflow == nil {
		return sr.store.UpdateWorkflowRunError(ctx, runID, "workflow not found")
	}

	// Parse the workflow
	parser := cwl.NewParser()
	docBytes, _ := json.Marshal(workflow.Document)
	doc, err := parser.ParseBytes(docBytes)
	if err != nil {
		return sr.store.UpdateWorkflowRunError(ctx, runID, fmt.Sprintf("failed to parse workflow: %v", err))
	}

	// Build DAG
	builder := dag.NewBuilder(doc, run.Inputs)
	workflowDAG, err := builder.Build(runID)
	if err != nil {
		return sr.store.UpdateWorkflowRunError(ctx, runID, fmt.Sprintf("failed to build DAG: %v", err))
	}

	// Create step executions in MongoDB
	for _, node := range workflowDAG.Nodes {
		stepExec := &state.StepExecution{
			WorkflowRunID: runID,
			StepID:        node.StepID,
			ScatterIndex:  node.ScatterIndex,
			Inputs:        node.Inputs,
		}
		if err := sr.store.CreateStepExecution(ctx, stepExec); err != nil {
			log.Printf("Error creating step execution: %v", err)
		}
	}

	// Save DAG state
	dagState := serializeDAG(workflowDAG)
	if err := sr.store.UpdateWorkflowRunDAGState(ctx, runID, dagState); err != nil {
		log.Printf("Error saving DAG state: %v", err)
	}

	// Update status to running
	if err := sr.store.UpdateWorkflowRunStatus(ctx, runID, state.WorkflowRunning); err != nil {
		return err
	}

	// Publish event
	sr.publisher.PublishWorkflowEvent(ctx, events.WorkflowEvent{
		Type:       "workflow_started",
		WorkflowID: runID,
		Status:     string(state.WorkflowRunning),
	})

	// Schedule ready nodes
	return sr.scheduleReadyNodes(ctx, workflowDAG, run)
}

// processRunningWorkflow processes a running workflow.
func (sr *SchedulerRunner) processRunningWorkflow(ctx context.Context, runID string) error {
	run, err := sr.store.GetWorkflowRun(ctx, runID)
	if err != nil || run == nil {
		return err
	}

	if run.DAGState == nil {
		return fmt.Errorf("workflow %s has no DAG state", runID)
	}

	// Get the workflow document
	workflow, err := sr.store.GetWorkflow(ctx, run.WorkflowID)
	if err != nil || workflow == nil {
		return err
	}

	// Rebuild DAG from state
	parser := cwl.NewParser()
	docBytes, _ := json.Marshal(workflow.Document)
	doc, err := parser.ParseBytes(docBytes)
	if err != nil {
		return err
	}

	builder := dag.NewBuilder(doc, run.Inputs)
	workflowDAG, err := builder.Build(runID)
	if err != nil {
		return err
	}

	// Restore DAG state
	restoreDAG(workflowDAG, run.DAGState)

	// Schedule ready nodes
	return sr.scheduleReadyNodes(ctx, workflowDAG, run)
}

// scheduleReadyNodes schedules ready nodes for execution.
func (sr *SchedulerRunner) scheduleReadyNodes(ctx context.Context, workflowDAG *dag.DAG, run *state.WorkflowRun) error {
	readyNodes := workflowDAG.GetReadyNodes()

	for _, node := range readyNodes {
		// Prepare inputs from completed dependencies
		inputs, err := dag.PrepareNodeInputs(workflowDAG, node, run.Inputs)
		if err != nil {
			log.Printf("Error preparing inputs for node %s: %v", node.ID, err)
			continue
		}
		node.Inputs = inputs

		// Execute the node
		if err := sr.executor.Execute(ctx, node); err != nil {
			log.Printf("Error executing node %s: %v", node.ID, err)
			workflowDAG.UpdateNodeStatus(node.ID, dag.StatusFailed)
			continue
		}

		workflowDAG.UpdateNodeStatus(node.ID, dag.StatusRunning)
	}

	// Save updated DAG state
	dagState := serializeDAG(workflowDAG)
	return sr.store.UpdateWorkflowRunDAGState(ctx, run.ID, dagState)
}

// serializeDAG serializes a DAG to state.
func serializeDAG(d *dag.DAG) *state.DAGState {
	dagState := &state.DAGState{
		Nodes: make(map[string]state.NodeState),
	}

	for id, node := range d.Nodes {
		dagState.Nodes[id] = state.NodeState{
			ID:           node.ID,
			StepID:       node.StepID,
			ScatterIndex: node.ScatterIndex,
			Status:       string(node.GetStatus()),
			TaskID:       node.GetTaskID(),
			Inputs:       node.Inputs,
			Outputs:      node.Outputs,
			Error:        node.Error,
		}
	}

	return dagState
}

// restoreDAG restores DAG state from storage.
func restoreDAG(d *dag.DAG, dagState *state.DAGState) {
	for id, nodeState := range dagState.Nodes {
		if node := d.GetNode(id); node != nil {
			node.SetStatus(dag.NodeStatus(nodeState.Status))
			node.SetTaskID(nodeState.TaskID)
			node.Outputs = nodeState.Outputs
			node.Error = nodeState.Error
		}
	}
}
