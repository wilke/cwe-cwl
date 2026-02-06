package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/cwl"
	"github.com/BV-BRC/cwe-cwl/internal/dag"
	"github.com/BV-BRC/cwe-cwl/internal/state"
	"github.com/BV-BRC/cwe-cwl/pkg/auth"
)

// Handler contains all HTTP handlers.
type Handler struct {
	config    *config.Config
	store     *state.Store
	validator *auth.TokenValidator
	parser    *cwl.Parser
}

// NewHandler creates a new handler.
func NewHandler(cfg *config.Config, store *state.Store, validator *auth.TokenValidator) *Handler {
	return &Handler{
		config:    cfg,
		store:     store,
		validator: validator,
		parser:    cwl.NewParser(),
	}
}

// HealthCheck handles health check requests.
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"service": "cwe-cwl",
	})
}

// SubmitWorkflow handles workflow submission.
func (h *Handler) SubmitWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.GetUserFromContext(ctx)

	var req state.SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Parse the workflow document
	var doc *cwl.Document
	var workflowID string
	var contentHash string

	switch wf := req.Workflow.(type) {
	case string:
		// Workflow ID reference
		workflow, err := h.store.GetWorkflow(ctx, wf)
		if err != nil {
			h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
			return
		}
		if workflow == nil {
			h.errorResponse(w, "workflow not found", http.StatusNotFound)
			return
		}
		workflowID = workflow.WorkflowID
		contentHash = workflow.ContentHash

		// Parse stored document
		docBytes, _ := json.Marshal(workflow.Document)
		doc, err = h.parser.ParseBytes(docBytes)
		if err != nil {
			h.errorResponse(w, "failed to parse stored workflow", http.StatusInternalServerError)
			return
		}

	case map[string]interface{}:
		// Inline workflow document
		docBytes, _ := json.Marshal(wf)
		var err error
		doc, err = h.parser.ParseBytes(docBytes)
		if err != nil {
			h.errorResponse(w, fmt.Sprintf("failed to parse workflow: %v", err), http.StatusBadRequest)
			return
		}

		contentHash = cwl.ContentHash(docBytes)
		workflowID = doc.ID
		if workflowID == "" {
			workflowID = contentHash
		}

		// Store the workflow
		storedWf := &state.Workflow{
			WorkflowID:  workflowID,
			ContentHash: contentHash,
			CWLVersion:  doc.CWLVersion,
			Document:    wf,
		}
		if err := h.store.SaveWorkflow(ctx, storedWf); err != nil {
			h.errorResponse(w, "failed to save workflow", http.StatusInternalServerError)
			return
		}

	default:
		h.errorResponse(w, "workflow must be a string ID or a CWL document", http.StatusBadRequest)
		return
	}

	// Validate the workflow
	if doc.Class == cwl.ClassWorkflow {
		analyzer := cwl.NewWorkflowAnalyzer(doc)
		if errs := analyzer.ValidateWorkflow(); len(errs) > 0 {
			var errMsgs []string
			for _, e := range errs {
				errMsgs = append(errMsgs, e.Error())
			}
			h.errorResponse(w, fmt.Sprintf("workflow validation failed: %s", strings.Join(errMsgs, "; ")), http.StatusBadRequest)
			return
		}
	}

	// Validate input files are accessible
	if err := h.validateInputFiles(ctx, user.Token, req.Inputs); err != nil {
		h.errorResponse(w, fmt.Sprintf("input validation failed: %v", err), http.StatusBadRequest)
		return
	}

	// Create workflow run
	runID := uuid.New().String()
	owner := user.UserID
	if owner == "" {
		owner = user.Username
	}

	run := &state.WorkflowRun{
		ID:         runID,
		WorkflowID: workflowID,
		Owner:      owner,
		Inputs:     req.Inputs,
		OutputPath: req.OutputPath,
	}

	if err := h.store.CreateWorkflowRun(ctx, run); err != nil {
		h.errorResponse(w, "failed to create workflow run", http.StatusInternalServerError)
		return
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(state.SubmitResponse{
		ID:      runID,
		Status:  string(state.WorkflowPending),
		Message: "Workflow submitted successfully",
	})
}

// ListWorkflows handles listing user's workflows.
func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.GetUserFromContext(ctx)

	filter := state.WorkflowRunFilter{
		Owner:  user.UserID,
		Limit:  50,
		Offset: 0,
	}

	// Parse query parameters
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = status
	}
	if workflowID := r.URL.Query().Get("workflow_id"); workflowID != "" {
		filter.WorkflowID = workflowID
	}

	runs, err := h.store.ListWorkflowRuns(ctx, filter)
	if err != nil {
		h.errorResponse(w, "failed to list workflows", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// GetWorkflow handles getting workflow status.
func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.GetUserFromContext(ctx)
	id := chi.URLParam(r, "id")

	run, err := h.store.GetWorkflowRun(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
		return
	}
	if run == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}
	if !h.isOwner(user, run) {
		h.errorResponse(w, "forbidden", http.StatusForbidden)
		return
	}

	// Get progress
	progress, _ := h.store.GetRunProgress(ctx, id)

	response := map[string]interface{}{
		"id":           run.ID,
		"workflow_id":  run.WorkflowID,
		"status":       run.Status,
		"owner":        run.Owner,
		"inputs":       run.Inputs,
		"output_path":  run.OutputPath,
		"created_at":   run.CreatedAt,
		"started_at":   run.StartedAt,
		"completed_at": run.CompletedAt,
	}

	if run.ErrorMessage != "" {
		response["error_message"] = run.ErrorMessage
	}
	if run.Outputs != nil {
		response["outputs"] = run.Outputs
	}
	if progress != nil {
		response["progress"] = progress
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// CancelWorkflow handles workflow cancellation.
func (h *Handler) CancelWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.GetUserFromContext(ctx)
	id := chi.URLParam(r, "id")

	run, err := h.store.GetWorkflowRun(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
		return
	}
	if run == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}
	if !h.isOwner(user, run) {
		h.errorResponse(w, "forbidden", http.StatusForbidden)
		return
	}

	// Check if workflow can be cancelled
	if run.Status == state.WorkflowCompleted || run.Status == state.WorkflowFailed || run.Status == state.WorkflowCancelled {
		h.errorResponse(w, "workflow cannot be cancelled", http.StatusBadRequest)
		return
	}

	// Update status
	if err := h.store.UpdateWorkflowRunStatus(ctx, id, state.WorkflowCancelled); err != nil {
		h.errorResponse(w, "failed to cancel workflow", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "cancelled",
		"message": "Workflow cancelled successfully",
	})
}

// RerunWorkflow handles workflow rerun.
func (h *Handler) RerunWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.GetUserFromContext(ctx)
	id := chi.URLParam(r, "id")

	run, err := h.store.GetWorkflowRun(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
		return
	}
	if run == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}
	if !h.isOwner(user, run) {
		h.errorResponse(w, "forbidden", http.StatusForbidden)
		return
	}

	// Create new run with same inputs
	newRunID := uuid.New().String()
	owner := user.UserID
	if owner == "" {
		owner = user.Username
	}

	newRun := &state.WorkflowRun{
		ID:         newRunID,
		WorkflowID: run.WorkflowID,
		Owner:      owner,
		Inputs:     run.Inputs,
		OutputPath: run.OutputPath,
	}

	if err := h.store.CreateWorkflowRun(ctx, newRun); err != nil {
		h.errorResponse(w, "failed to create workflow run", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(state.SubmitResponse{
		ID:      newRunID,
		Status:  string(state.WorkflowPending),
		Message: "Workflow rerun submitted successfully",
	})
}

// GetWorkflowSteps handles getting all step statuses.
func (h *Handler) GetWorkflowSteps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.GetUserFromContext(ctx)
	id := chi.URLParam(r, "id")

	run, err := h.store.GetWorkflowRun(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
		return
	}
	if run == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}
	if !h.isOwner(user, run) {
		h.errorResponse(w, "forbidden", http.StatusForbidden)
		return
	}

	steps, err := h.store.ListStepExecutions(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow steps", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(steps)
}

// GetWorkflowOutputs handles getting workflow outputs.
func (h *Handler) GetWorkflowOutputs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.GetUserFromContext(ctx)
	id := chi.URLParam(r, "id")

	run, err := h.store.GetWorkflowRun(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
		return
	}
	if run == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}
	if !h.isOwner(user, run) {
		h.errorResponse(w, "forbidden", http.StatusForbidden)
		return
	}

	if run.Status != state.WorkflowCompleted {
		h.errorResponse(w, "workflow not completed", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run.Outputs)
}

// AdminListWorkflows lists workflows across users (admin-only).
func (h *Handler) AdminListWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter := state.WorkflowRunFilter{
		Limit:  50,
		Offset: 0,
	}

	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = status
	}
	if workflowID := r.URL.Query().Get("workflow_id"); workflowID != "" {
		filter.WorkflowID = workflowID
	}
	if owner := r.URL.Query().Get("owner"); owner != "" {
		filter.Owner = owner
	}
	if limit := parseIntQuery(r, "limit", 50); limit > 0 {
		filter.Limit = limit
	}
	if offset := parseIntQuery(r, "offset", 0); offset >= 0 {
		filter.Offset = offset
	}

	runs, err := h.store.ListWorkflowRuns(ctx, filter)
	if err != nil {
		h.errorResponse(w, "failed to list workflows", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// AdminGetWorkflow gets workflow details across users (admin-only).
func (h *Handler) AdminGetWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	run, err := h.store.GetWorkflowRun(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
		return
	}
	if run == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}

	progress, _ := h.store.GetRunProgress(ctx, id)

	response := map[string]interface{}{
		"id":           run.ID,
		"workflow_id":  run.WorkflowID,
		"status":       run.Status,
		"owner":        run.Owner,
		"inputs":       run.Inputs,
		"output_path":  run.OutputPath,
		"created_at":   run.CreatedAt,
		"started_at":   run.StartedAt,
		"completed_at": run.CompletedAt,
	}

	if run.ErrorMessage != "" {
		response["error_message"] = run.ErrorMessage
	}
	if run.Outputs != nil {
		response["outputs"] = run.Outputs
	}
	if progress != nil {
		response["progress"] = progress
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// AdminCancelWorkflow cancels a workflow across users (admin-only).
func (h *Handler) AdminCancelWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	run, err := h.store.GetWorkflowRun(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
		return
	}
	if run == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}

	if run.Status == state.WorkflowCompleted || run.Status == state.WorkflowFailed || run.Status == state.WorkflowCancelled {
		h.errorResponse(w, "workflow cannot be cancelled", http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateWorkflowRunStatus(ctx, id, state.WorkflowCancelled); err != nil {
		h.errorResponse(w, "failed to cancel workflow", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "cancelled",
		"message": "Workflow cancelled successfully",
	})
}

// AdminRerunWorkflow reruns a workflow across users (admin-only).
func (h *Handler) AdminRerunWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	run, err := h.store.GetWorkflowRun(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
		return
	}
	if run == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}

	owner := run.Owner
	if override := r.URL.Query().Get("owner"); override != "" {
		owner = override
	}

	newRunID := uuid.New().String()
	newRun := &state.WorkflowRun{
		ID:         newRunID,
		WorkflowID: run.WorkflowID,
		Owner:      owner,
		Inputs:     run.Inputs,
		OutputPath: run.OutputPath,
	}

	if err := h.store.CreateWorkflowRun(ctx, newRun); err != nil {
		h.errorResponse(w, "failed to create workflow run", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(state.SubmitResponse{
		ID:      newRunID,
		Status:  string(state.WorkflowPending),
		Message: "Workflow rerun submitted successfully",
	})
}

// AdminGetWorkflowSteps lists all step executions for a workflow run (admin-only).
func (h *Handler) AdminGetWorkflowSteps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	steps, err := h.store.ListStepExecutionsFull(ctx, id)
	if err != nil {
		h.errorResponse(w, "failed to get workflow steps", http.StatusInternalServerError)
		return
	}

	resp := make([]map[string]interface{}, 0, len(steps))
	for _, step := range steps {
		resp = append(resp, map[string]interface{}{
			"id":              step.ID.Hex(),
			"workflow_run_id": step.WorkflowRunID,
			"step_id":         step.StepID,
			"scatter_index":   step.ScatterIndex,
			"status":          step.Status,
			"bvbrc_task_id":   step.BVBRCTaskID,
			"inputs":          step.Inputs,
			"outputs":         step.Outputs,
			"error_message":   step.ErrorMessage,
			"created_at":      step.CreatedAt,
			"started_at":      step.StartedAt,
			"completed_at":    step.CompletedAt,
			"retry_count":     step.RetryCount,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// AdminRequeueStep resets a step execution for re-run (admin-only).
func (h *Handler) AdminRequeueStep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := chi.URLParam(r, "id")
	stepID := chi.URLParam(r, "step_id")

	scatterIndex, err := parseScatterIndex(r.URL.Query().Get("scatter_index"))
	if err != nil {
		h.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	run, err := h.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		h.errorResponse(w, "failed to get workflow", http.StatusInternalServerError)
		return
	}
	if run == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}
	if run.Status == state.WorkflowCompleted || run.Status == state.WorkflowCancelled {
		h.errorResponse(w, "workflow cannot be requeued in current state", http.StatusBadRequest)
		return
	}
	if run.DAGState == nil {
		h.errorResponse(w, "workflow has no DAG state", http.StatusBadRequest)
		return
	}

	exec, err := h.store.GetStepExecutionByStep(ctx, runID, stepID, scatterIndex)
	if err != nil {
		h.errorResponse(w, "failed to get step execution", http.StatusInternalServerError)
		return
	}
	if exec == nil {
		h.errorResponse(w, "step execution not found", http.StatusNotFound)
		return
	}

	workflow, err := h.store.GetWorkflow(ctx, run.WorkflowID)
	if err != nil || workflow == nil {
		h.errorResponse(w, "workflow not found", http.StatusNotFound)
		return
	}

	parser := cwl.NewParser()
	docBytes, _ := json.Marshal(workflow.Document)
	doc, err := parser.ParseBytes(docBytes)
	if err != nil {
		h.errorResponse(w, "failed to parse workflow", http.StatusInternalServerError)
		return
	}

	builder := dag.NewBuilder(doc, run.Inputs)
	workflowDAG, err := builder.Build(runID)
	if err != nil {
		h.errorResponse(w, "failed to build DAG", http.StatusInternalServerError)
		return
	}

	restoreDAGFromState(workflowDAG, run.DAGState)

	node := findNodeByStep(workflowDAG, stepID, scatterIndex)
	if node == nil {
		h.errorResponse(w, "step node not found in DAG", http.StatusNotFound)
		return
	}

	// Prevent requeue if dependents already completed.
	for _, depID := range node.Dependents {
		dep := workflowDAG.GetNode(depID)
		if dep != nil && dep.GetStatus() == dag.StatusCompleted {
			h.errorResponse(w, "cannot requeue step with completed dependents", http.StatusConflict)
			return
		}
	}

	// Reset node state.
	node.SetTaskID("")
	node.Outputs = nil
	node.Error = ""

	if dependenciesSatisfied(workflowDAG, node) {
		node.SetStatus(dag.StatusReady)
	} else {
		node.SetStatus(dag.StatusPending)
	}

	// Update DAG state in workflow run.
	if run.DAGState.Nodes == nil {
		run.DAGState.Nodes = make(map[string]state.NodeState)
	}
	run.DAGState.Nodes[node.ID] = state.NodeState{
		ID:           node.ID,
		StepID:       node.StepID,
		ScatterIndex: node.ScatterIndex,
		Status:       string(node.GetStatus()),
		TaskID:       node.GetTaskID(),
		Inputs:       node.Inputs,
		Outputs:      node.Outputs,
		Error:        node.Error,
	}
	if err := h.store.UpdateWorkflowRunDAGState(ctx, run.ID, run.DAGState); err != nil {
		h.errorResponse(w, "failed to update DAG state", http.StatusInternalServerError)
		return
	}

	if err := h.store.ResetStepExecution(ctx, exec.ID, true); err != nil {
		h.errorResponse(w, "failed to reset step execution", http.StatusInternalServerError)
		return
	}

	if run.Status == state.WorkflowFailed {
		if err := h.store.UpdateWorkflowRunStatus(ctx, run.ID, state.WorkflowRunning); err != nil {
			h.errorResponse(w, "failed to update workflow status", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "requeued",
		"message": "Step requeued successfully",
	})
}

// ValidateCWL handles CWL document validation.
func (h *Handler) ValidateCWL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Document interface{} `json:"document"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	docBytes, _ := json.Marshal(req.Document)
	doc, err := h.parser.ParseBytes(docBytes)

	result := state.ValidationResult{Valid: true}

	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	} else if doc.Class == cwl.ClassWorkflow {
		analyzer := cwl.NewWorkflowAnalyzer(doc)
		if errs := analyzer.ValidateWorkflow(); len(errs) > 0 {
			result.Valid = false
			for _, e := range errs {
				result.Errors = append(result.Errors, e.Error())
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ValidateInputs handles input file validation.
func (h *Handler) ValidateInputs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.GetUserFromContext(ctx)

	var req struct {
		Inputs map[string]interface{} `json:"inputs"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	err := h.validateInputFiles(ctx, user.Token, req.Inputs)

	result := state.ValidationResult{Valid: err == nil}
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// UploadFile handles file uploads to local storage.
func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form
	if err := r.ParseMultipartForm(100 << 20); err != nil { // 100MB max
		h.errorResponse(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		h.errorResponse(w, "failed to get file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Generate unique file ID
	fileID := uuid.New().String()
	ext := filepath.Ext(header.Filename)
	localPath := filepath.Join(h.config.Storage.LocalPath, fileID+ext)

	// Ensure directory exists
	if err := os.MkdirAll(h.config.Storage.LocalPath, 0755); err != nil {
		h.errorResponse(w, "failed to create storage directory", http.StatusInternalServerError)
		return
	}

	// Create local file
	dst, err := os.Create(localPath)
	if err != nil {
		h.errorResponse(w, "failed to create file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy content
	size, err := io.Copy(dst, file)
	if err != nil {
		h.errorResponse(w, "failed to save file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       fileID,
		"filename": header.Filename,
		"size":     size,
		"path":     localPath,
	})
}

// DownloadFile handles file downloads from local storage.
func (h *Handler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Find file in storage directory
	matches, err := filepath.Glob(filepath.Join(h.config.Storage.LocalPath, id+".*"))
	if err != nil || len(matches) == 0 {
		h.errorResponse(w, "file not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, matches[0])
}

// validateInputFiles validates that all input files are accessible.
func (h *Handler) validateInputFiles(ctx context.Context, token string, inputs map[string]interface{}) error {
	for key, val := range inputs {
		if err := h.validateInputValue(ctx, token, key, val); err != nil {
			return err
		}
	}
	return nil
}

// validateInputValue recursively validates input values.
func (h *Handler) validateInputValue(ctx context.Context, token string, key string, val interface{}) error {
	switch v := val.(type) {
	case map[string]interface{}:
		if class, ok := v["class"].(string); ok {
			if class == cwl.TypeFile || class == cwl.TypeDirectory {
				// Validate file/directory access
				if path, ok := v["path"].(string); ok {
					if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, h.config.Storage.LocalPath) {
						// Workspace path
						if err := h.validator.ValidateWorkspaceAccess(ctx, token, path); err != nil {
							return fmt.Errorf("input %s: %w", key, err)
						}
					}
				}
				if loc, ok := v["location"].(string); ok {
					if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, h.config.Storage.LocalPath) {
						// Workspace path
						if err := h.validator.ValidateWorkspaceAccess(ctx, token, loc); err != nil {
							return fmt.Errorf("input %s: %w", key, err)
						}
					}
				}
			}
		}
		// Recurse into nested objects
		for k, nested := range v {
			if err := h.validateInputValue(ctx, token, key+"."+k, nested); err != nil {
				return err
			}
		}
	case []interface{}:
		for i, item := range v {
			if err := h.validateInputValue(ctx, token, fmt.Sprintf("%s[%d]", key, i), item); err != nil {
				return err
			}
		}
	}
	return nil
}

// errorResponse sends an error response.
func (h *Handler) errorResponse(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

func (h *Handler) isOwner(user *auth.UserInfo, run *state.WorkflowRun) bool {
	if user == nil || run == nil {
		return true
	}
	return run.Owner == user.UserID || run.Owner == user.Username || run.Owner == user.Email
}

func parseIntQuery(r *http.Request, key string, def int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return def
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return parsed
}

func parseScatterIndex(raw string) ([]int, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	indices := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid scatter_index value: %s", raw)
		}
		indices = append(indices, v)
	}
	return indices, nil
}

func restoreDAGFromState(d *dag.DAG, dagState *state.DAGState) {
	if d == nil || dagState == nil {
		return
	}
	for id, nodeState := range dagState.Nodes {
		if node := d.GetNode(id); node != nil {
			node.SetStatus(dag.NodeStatus(nodeState.Status))
			node.SetTaskID(nodeState.TaskID)
			node.Outputs = nodeState.Outputs
			node.Error = nodeState.Error
		}
	}
}

func findNodeByStep(d *dag.DAG, stepID string, scatterIndex []int) *dag.Node {
	for _, node := range d.Nodes {
		if node.StepID != stepID {
			continue
		}
		if scatterIndexEqual(node.ScatterIndex, scatterIndex) {
			return node
		}
	}
	return nil
}

func scatterIndexEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func dependenciesSatisfied(d *dag.DAG, node *dag.Node) bool {
	for _, depID := range node.Dependencies {
		dep := d.GetNode(depID)
		if dep == nil {
			return false
		}
		status := dep.GetStatus()
		if status != dag.StatusCompleted && status != dag.StatusSkipped {
			return false
		}
	}
	return true
}
