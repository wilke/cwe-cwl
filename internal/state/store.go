package state

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Store provides MongoDB state management for CWL workflows.
type Store struct {
	client   *mongo.Client
	database *mongo.Database

	workflows       *mongo.Collection
	workflowRuns    *mongo.Collection
	stepExecutions  *mongo.Collection
	containerMaps   *mongo.Collection
}

// NewStore creates a new MongoDB store.
func NewStore(uri, dbName string) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	db := client.Database(dbName)

	store := &Store{
		client:          client,
		database:        db,
		workflows:       db.Collection("workflows"),
		workflowRuns:    db.Collection("workflow_runs"),
		stepExecutions:  db.Collection("step_executions"),
		containerMaps:   db.Collection("container_mappings"),
	}

	// Create indexes
	if err := store.createIndexes(ctx); err != nil {
		return nil, err
	}

	return store, nil
}

// createIndexes creates necessary indexes for the collections.
func (s *Store) createIndexes(ctx context.Context) error {
	// Workflows indexes
	_, err := s.workflows.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "workflow_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "content_hash", Value: 1}},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create workflows indexes: %w", err)
	}

	// WorkflowRuns indexes
	_, err = s.workflowRuns.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "owner", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "workflow_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create workflow_runs indexes: %w", err)
	}

	// StepExecutions indexes
	_, err = s.stepExecutions.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "workflow_run_id", Value: 1}},
		},
		{
			Keys: bson.D{
				{Key: "workflow_run_id", Value: 1},
				{Key: "step_id", Value: 1},
			},
		},
		{
			Keys: bson.D{{Key: "bvbrc_task_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create step_executions indexes: %w", err)
	}

	return nil
}

// Close closes the MongoDB connection.
func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// Workflow operations

// SaveWorkflow saves or updates a CWL workflow document.
func (s *Store) SaveWorkflow(ctx context.Context, wf *Workflow) error {
	wf.CreatedAt = time.Now()

	opts := options.Update().SetUpsert(true)
	filter := bson.M{"workflow_id": wf.WorkflowID}
	update := bson.M{"$set": wf}

	_, err := s.workflows.UpdateOne(ctx, filter, update, opts)
	return err
}

// GetWorkflow retrieves a workflow by ID.
func (s *Store) GetWorkflow(ctx context.Context, workflowID string) (*Workflow, error) {
	var wf Workflow
	err := s.workflows.FindOne(ctx, bson.M{"workflow_id": workflowID}).Decode(&wf)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &wf, nil
}

// GetWorkflowByHash retrieves a workflow by content hash.
func (s *Store) GetWorkflowByHash(ctx context.Context, hash string) (*Workflow, error) {
	var wf Workflow
	err := s.workflows.FindOne(ctx, bson.M{"content_hash": hash}).Decode(&wf)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &wf, nil
}

// WorkflowRun operations

// CreateWorkflowRun creates a new workflow run.
func (s *Store) CreateWorkflowRun(ctx context.Context, run *WorkflowRun) error {
	run.CreatedAt = time.Now()
	run.Status = WorkflowPending

	_, err := s.workflowRuns.InsertOne(ctx, run)
	return err
}

// GetWorkflowRun retrieves a workflow run by ID.
func (s *Store) GetWorkflowRun(ctx context.Context, id string) (*WorkflowRun, error) {
	var run WorkflowRun
	err := s.workflowRuns.FindOne(ctx, bson.M{"_id": id}).Decode(&run)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// UpdateWorkflowRunStatus updates the status of a workflow run.
func (s *Store) UpdateWorkflowRunStatus(ctx context.Context, id string, status WorkflowStatus) error {
	update := bson.M{
		"$set": bson.M{"status": status},
	}

	switch status {
	case WorkflowRunning:
		now := time.Now()
		update["$set"].(bson.M)["started_at"] = now
	case WorkflowCompleted, WorkflowFailed, WorkflowCancelled:
		now := time.Now()
		update["$set"].(bson.M)["completed_at"] = now
	}

	_, err := s.workflowRuns.UpdateOne(ctx, bson.M{"_id": id}, update)
	return err
}

// UpdateWorkflowRunError updates the error message of a workflow run.
func (s *Store) UpdateWorkflowRunError(ctx context.Context, id string, errMsg string) error {
	update := bson.M{
		"$set": bson.M{
			"error_message": errMsg,
			"status":        WorkflowFailed,
			"completed_at":  time.Now(),
		},
	}
	_, err := s.workflowRuns.UpdateOne(ctx, bson.M{"_id": id}, update)
	return err
}

// UpdateWorkflowRunOutputs updates the outputs of a workflow run.
func (s *Store) UpdateWorkflowRunOutputs(ctx context.Context, id string, outputs map[string]interface{}) error {
	update := bson.M{
		"$set": bson.M{"outputs": outputs},
	}
	_, err := s.workflowRuns.UpdateOne(ctx, bson.M{"_id": id}, update)
	return err
}

// UpdateWorkflowRunDAGState updates the DAG state for recovery.
func (s *Store) UpdateWorkflowRunDAGState(ctx context.Context, id string, dagState *DAGState) error {
	update := bson.M{
		"$set": bson.M{"dag_state": dagState},
	}
	_, err := s.workflowRuns.UpdateOne(ctx, bson.M{"_id": id}, update)
	return err
}

// ListWorkflowRuns lists workflow runs with filtering and pagination.
func (s *Store) ListWorkflowRuns(ctx context.Context, filter WorkflowRunFilter) ([]WorkflowRunSummary, error) {
	query := bson.M{}

	if filter.Owner != "" {
		query["owner"] = filter.Owner
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.WorkflowID != "" {
		query["workflow_id"] = filter.WorkflowID
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(filter.Limit)).
		SetSkip(int64(filter.Offset))

	// Project only summary fields
	opts.SetProjection(bson.M{
		"_id":          1,
		"workflow_id":  1,
		"status":       1,
		"owner":        1,
		"output_path":  1,
		"created_at":   1,
		"completed_at": 1,
	})

	cursor, err := s.workflowRuns.Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var runs []WorkflowRunSummary
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, err
	}

	return runs, nil
}

// WorkflowRunFilter defines filtering options for workflow runs.
type WorkflowRunFilter struct {
	Owner      string
	Status     string
	WorkflowID string
	Limit      int
	Offset     int
}

// StepExecution operations

// CreateStepExecution creates a new step execution.
func (s *Store) CreateStepExecution(ctx context.Context, exec *StepExecution) error {
	exec.ID = primitive.NewObjectID()
	exec.CreatedAt = time.Now()
	exec.Status = StepPending

	_, err := s.stepExecutions.InsertOne(ctx, exec)
	return err
}

// GetStepExecution retrieves a step execution by ID.
func (s *Store) GetStepExecution(ctx context.Context, id primitive.ObjectID) (*StepExecution, error) {
	var exec StepExecution
	err := s.stepExecutions.FindOne(ctx, bson.M{"_id": id}).Decode(&exec)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

// GetStepExecutionByTaskID retrieves a step execution by BV-BRC task ID.
func (s *Store) GetStepExecutionByTaskID(ctx context.Context, taskID int64) (*StepExecution, error) {
	var exec StepExecution
	err := s.stepExecutions.FindOne(ctx, bson.M{"bvbrc_task_id": taskID}).Decode(&exec)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

// UpdateStepExecution updates a step execution.
func (s *Store) UpdateStepExecution(ctx context.Context, id primitive.ObjectID, update *StepExecutionUpdate) error {
	updateDoc := bson.M{}

	if update.Status != "" {
		updateDoc["status"] = update.Status
	}
	if update.BVBRCTaskID != 0 {
		updateDoc["bvbrc_task_id"] = update.BVBRCTaskID
	}
	if update.Outputs != nil {
		updateDoc["outputs"] = update.Outputs
	}
	if update.ErrorMessage != "" {
		updateDoc["error_message"] = update.ErrorMessage
	}
	if update.SetStarted {
		now := time.Now()
		updateDoc["started_at"] = now
	}
	if update.SetCompleted {
		now := time.Now()
		updateDoc["completed_at"] = now
	}
	if update.IncrRetry {
		_, err := s.stepExecutions.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
			"$inc": bson.M{"retry_count": 1},
			"$set": updateDoc,
		})
		return err
	}

	_, err := s.stepExecutions.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": updateDoc})
	return err
}

// StepExecutionUpdate defines fields to update on a step execution.
type StepExecutionUpdate struct {
	Status       StepStatus
	BVBRCTaskID  int64
	Outputs      map[string]interface{}
	ErrorMessage string
	SetStarted   bool
	SetCompleted bool
	IncrRetry    bool
}

// ListStepExecutions lists step executions for a workflow run.
func (s *Store) ListStepExecutions(ctx context.Context, workflowRunID string) ([]StepExecutionSummary, error) {
	opts := options.Find().
		SetProjection(bson.M{
			"step_id":       1,
			"scatter_index": 1,
			"status":        1,
			"bvbrc_task_id": 1,
			"started_at":    1,
			"completed_at":  1,
		})

	cursor, err := s.stepExecutions.Find(ctx, bson.M{"workflow_run_id": workflowRunID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var execs []StepExecutionSummary
	if err := cursor.All(ctx, &execs); err != nil {
		return nil, err
	}

	return execs, nil
}

// Container mapping operations

// SaveContainerMapping saves or updates a container mapping.
func (s *Store) SaveContainerMapping(ctx context.Context, mapping *ContainerMapping) error {
	mapping.CreatedAt = time.Now()

	opts := options.Update().SetUpsert(true)
	filter := bson.M{"_id": mapping.ID}
	update := bson.M{"$set": mapping}

	_, err := s.containerMaps.UpdateOne(ctx, filter, update, opts)
	return err
}

// GetContainerMapping retrieves a container mapping by Docker image.
func (s *Store) GetContainerMapping(ctx context.Context, dockerImage string) (*ContainerMapping, error) {
	var mapping ContainerMapping
	err := s.containerMaps.FindOne(ctx, bson.M{"_id": dockerImage}).Decode(&mapping)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &mapping, nil
}

// GetRunProgress calculates progress for a workflow run.
func (s *Store) GetRunProgress(ctx context.Context, workflowRunID string) (*RunProgress, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"workflow_run_id": workflowRunID}}},
		{{Key: "$group", Value: bson.M{
			"_id":       "$status",
			"count":     bson.M{"$sum": 1},
		}}},
	}

	cursor, err := s.stepExecutions.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	progress := &RunProgress{}
	for cursor.Next(ctx) {
		var result struct {
			Status string `bson:"_id"`
			Count  int    `bson:"count"`
		}
		if err := cursor.Decode(&result); err != nil {
			return nil, err
		}

		switch StepStatus(result.Status) {
		case StepPending:
			progress.Pending = result.Count
		case StepRunning:
			progress.Running = result.Count
		case StepCompleted:
			progress.Completed = result.Count
		case StepFailed:
			progress.Failed = result.Count
		case StepSkipped:
			progress.Skipped = result.Count
		}
		progress.Total += result.Count
	}

	return progress, nil
}
