package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BV-BRC/cwe-cwl/internal/bvbrc"
	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

func TestJSONRPCClient_Call(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("Content-Type") != "application/jsonrpc+json" {
			t.Errorf("Expected Content-Type application/jsonrpc+json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "test-token" {
			t.Errorf("Expected Authorization test-token, got %s", r.Header.Get("Authorization"))
		}

		// Parse request
		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Verify request format
		if req.JSONRPC != "2.0" {
			t.Errorf("Expected JSONRPC 2.0, got %s", req.JSONRPC)
		}
		if req.Method != "AppService.test_method" {
			t.Errorf("Expected method AppService.test_method, got %s", req.Method)
		}

		// Send response
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"status": "ok"}`),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client and make call
	client := NewJSONRPCClient(server.URL)
	result, err := client.Call(context.Background(), "test-token", "AppService.test_method", []interface{}{"arg1", "arg2"})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Verify result
	var resultMap map[string]string
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}
	if resultMap["status"] != "ok" {
		t.Errorf("Expected status ok, got %s", resultMap["status"])
	}
}

func TestJSONRPCClient_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32600,
				Message: "Invalid request",
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewJSONRPCClient(server.URL)
	_, err := client.Call(context.Background(), "test-token", "AppService.bad_method", nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	rpcErr, ok := err.(*JSONRPCError)
	if !ok {
		t.Fatalf("Expected JSONRPCError, got %T", err)
	}
	if rpcErr.Code != -32600 {
		t.Errorf("Expected error code -32600, got %d", rpcErr.Code)
	}
}

func TestJSONRPCClient_StartApp2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify method
		if req.Method != "AppService.start_app2" {
			t.Errorf("Expected method AppService.start_app2, got %s", req.Method)
		}

		// Verify params structure
		if len(req.Params) != 3 {
			t.Errorf("Expected 3 params, got %d", len(req.Params))
		}

		// Return mock task
		task := BVBRCTask{
			ID:         "task-12345",
			App:        "CWLStepRunner",
			Status:     "submitted",
			UserID:     "user@example.org",
			SubmitTime: "2026-02-06T12:00:00Z",
		}
		taskJSON, _ := json.Marshal([]BVBRCTask{task})

		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  taskJSON,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewJSONRPCClient(server.URL)
	task, err := client.StartApp2(
		context.Background(),
		"test-token",
		"CWLStepRunner",
		map[string]string{
			"cwl_command": "[\"echo\", \"hello\"]",
			"output_path": "/user/home/output",
		},
		StartParams{
			BaseURL: "https://www.bv-brc.org",
		},
	)
	if err != nil {
		t.Fatalf("StartApp2 failed: %v", err)
	}

	if task.ID != "task-12345" {
		t.Errorf("Expected task ID task-12345, got %s", task.ID)
	}
	if task.Status != "submitted" {
		t.Errorf("Expected status submitted, got %s", task.Status)
	}
}

func TestJSONRPCClient_QueryTaskStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method != "AppService.query_task_status" {
			t.Errorf("Expected method AppService.query_task_status, got %s", req.Method)
		}

		task := BVBRCTask{
			ID:        "task-12345",
			Status:    "completed",
			StartTime: "2026-02-06T12:00:00Z",
			CompletedTime: "2026-02-06T12:05:00Z",
		}
		taskJSON, _ := json.Marshal(task)

		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  taskJSON,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewJSONRPCClient(server.URL)
	task, err := client.QueryTaskStatus(context.Background(), "test-token", "task-12345")
	if err != nil {
		t.Fatalf("QueryTaskStatus failed: %v", err)
	}

	if task.Status != "completed" {
		t.Errorf("Expected status completed, got %s", task.Status)
	}
}

func TestBVBRCExecutor_SubmitJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify params contain CWL job spec
		params := req.Params[1].(map[string]interface{})
		if _, ok := params["cwl_job_spec"]; !ok {
			t.Error("Expected cwl_job_spec in params")
		}
		if _, ok := params["output_path"]; !ok {
			t.Error("Expected output_path in params")
		}

		task := BVBRCTask{
			ID:     "task-cwl-12345",
			Status: "submitted",
		}
		taskJSON, _ := json.Marshal([]BVBRCTask{task})

		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  taskJSON,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := DefaultBVBRCExecutorConfig()
	cfg.AppServiceURL = server.URL

	executor := NewBVBRCExecutor(cfg)

	// Create a CWL tool document
	doc := &cwl.Document{
		Class: "CommandLineTool",
		ID:    "bwa-mem",
		Inputs: []cwl.Input{
			{ID: "reference", Type: "File"},
		},
	}

	jobSpec, err := bvbrc.NewCWLJobSpec(doc, map[string]interface{}{
		"reference": map[string]interface{}{
			"class": "File",
			"path":  "/data/ref.fa",
		},
	}, "/user/home/output")
	if err != nil {
		t.Fatalf("NewCWLJobSpec failed: %v", err)
	}

	taskID, err := executor.SubmitJob(context.Background(), "test-token", jobSpec)
	if err != nil {
		t.Fatalf("SubmitJob failed: %v", err)
	}

	if taskID != "task-cwl-12345" {
		t.Errorf("Expected task ID task-cwl-12345, got %s", taskID)
	}
}

func TestTaskStatusToDAGStatus(t *testing.T) {
	tests := []struct {
		bvbrcStatus string
		dagStatus   string
	}{
		{"queued", "pending"},
		{"submitted", "pending"},
		{"in-progress", "running"},
		{"running", "running"},
		{"completed", "completed"},
		{"failed", "failed"},
		{"deleted", "cancelled"},
		{"cancelled", "cancelled"},
		{"unknown", "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.bvbrcStatus, func(t *testing.T) {
			result := TaskStatusToDAGStatus(tc.bvbrcStatus)
			if result != tc.dagStatus {
				t.Errorf("TaskStatusToDAGStatus(%s) = %s, expected %s", tc.bvbrcStatus, result, tc.dagStatus)
			}
		})
	}
}
