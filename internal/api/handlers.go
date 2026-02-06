package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/cwl"
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
	id := chi.URLParam(r, "id")

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

	if run.Status != state.WorkflowCompleted {
		h.errorResponse(w, "workflow not completed", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run.Outputs)
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
