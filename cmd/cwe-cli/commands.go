package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Client wraps HTTP client for API calls.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new API client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// doRequest makes an authenticated HTTP request.
func (c *Client) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

// getClient creates a client from cobra command flags.
func getClient(cmd *cobra.Command) *Client {
	server, _ := cmd.Flags().GetString("server")
	token, _ := cmd.Flags().GetString("token")

	// Try to get token from environment if not provided
	if token == "" {
		token = os.Getenv("BVBRC_TOKEN")
	}
	if token == "" {
		token = os.Getenv("P3_TOKEN")
	}

	return NewClient(server, token)
}

func newSubmitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit <workflow.cwl> <job.yaml>",
		Short: "Submit a CWL workflow",
		Long:  `Submit a CWL workflow document with a job file containing inputs`,
		Args:  cobra.ExactArgs(2),
		RunE:  runSubmit,
	}

	cmd.Flags().StringP("output", "o", "", "Output path in Workspace")
	cmd.Flags().StringP("name", "n", "", "Workflow name")

	return cmd
}

func runSubmit(cmd *cobra.Command, args []string) error {
	workflowPath := args[0]
	jobPath := args[1]

	// Read workflow document
	workflowData, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("failed to read workflow file: %w", err)
	}

	var workflow interface{}
	if filepath.Ext(workflowPath) == ".json" {
		if err := json.Unmarshal(workflowData, &workflow); err != nil {
			return fmt.Errorf("failed to parse workflow JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(workflowData, &workflow); err != nil {
			return fmt.Errorf("failed to parse workflow YAML: %w", err)
		}
	}

	// Read job file (inputs)
	jobData, err := os.ReadFile(jobPath)
	if err != nil {
		return fmt.Errorf("failed to read job file: %w", err)
	}

	var inputs map[string]interface{}
	if filepath.Ext(jobPath) == ".json" {
		if err := json.Unmarshal(jobData, &inputs); err != nil {
			return fmt.Errorf("failed to parse job JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(jobData, &inputs); err != nil {
			return fmt.Errorf("failed to parse job YAML: %w", err)
		}
	}

	// Build request
	outputPath, _ := cmd.Flags().GetString("output")
	name, _ := cmd.Flags().GetString("name")

	reqBody := map[string]interface{}{
		"workflow": workflow,
		"inputs":   inputs,
	}
	if outputPath != "" {
		reqBody["output_path"] = outputPath
	}
	if name != "" {
		reqBody["name"] = name
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	// Submit
	client := getClient(cmd)
	resp, err := client.doRequest("POST", "/api/v1/workflows", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to submit workflow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("submission failed: %s", string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	fmt.Printf("Workflow submitted successfully\n")
	fmt.Printf("ID: %s\n", result["id"])
	fmt.Printf("Status: %s\n", result["status"])

	return nil
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <workflow-id>",
		Short: "Get workflow status",
		Args:  cobra.ExactArgs(1),
		RunE:  runStatus,
	}

	cmd.Flags().BoolP("watch", "w", false, "Watch for status changes")

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	id := args[0]
	watch, _ := cmd.Flags().GetBool("watch")

	client := getClient(cmd)

	for {
		resp, err := client.doRequest("GET", "/api/v1/workflows/"+id, nil)
		if err != nil {
			return err
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to get status: %v", result["error"])
		}

		fmt.Printf("Workflow: %s\n", result["id"])
		fmt.Printf("Status: %s\n", result["status"])

		if progress, ok := result["progress"].(map[string]interface{}); ok {
			total := int(progress["total"].(float64))
			completed := int(progress["completed"].(float64))
			running := int(progress["running"].(float64))
			failed := int(progress["failed"].(float64))
			fmt.Printf("Progress: %d/%d completed, %d running, %d failed\n", completed, total, running, failed)
		}

		if !watch {
			break
		}

		status := result["status"].(string)
		if status == "completed" || status == "failed" || status == "cancelled" {
			break
		}

		time.Sleep(5 * time.Second)
		fmt.Println("---")
	}

	return nil
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workflows",
		RunE:  runList,
	}

	cmd.Flags().StringP("status", "S", "", "Filter by status")

	return cmd
}

func runList(cmd *cobra.Command, args []string) error {
	client := getClient(cmd)

	path := "/api/v1/workflows"
	if status, _ := cmd.Flags().GetString("status"); status != "" {
		path += "?status=" + status
	}

	resp, err := client.doRequest("GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var results []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println("No workflows found")
		return nil
	}

	fmt.Printf("%-36s  %-12s  %-20s  %s\n", "ID", "STATUS", "CREATED", "WORKFLOW")
	for _, r := range results {
		created := r["created_at"].(string)
		if len(created) > 19 {
			created = created[:19]
		}
		fmt.Printf("%-36s  %-12s  %-20s  %s\n",
			r["id"], r["status"], created, r["workflow_id"])
	}

	return nil
}

func newCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <workflow-id>",
		Short: "Cancel a running workflow",
		Args:  cobra.ExactArgs(1),
		RunE:  runCancel,
	}
}

func runCancel(cmd *cobra.Command, args []string) error {
	id := args[0]
	client := getClient(cmd)

	resp, err := client.doRequest("DELETE", "/api/v1/workflows/"+id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to cancel: %v", result["error"])
	}

	fmt.Printf("Workflow %s cancelled\n", id)
	return nil
}

func newUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file> [workspace-path]",
		Short: "Upload a file to Workspace or local storage",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runUpload,
	}
}

func runUpload(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return err
	}

	if _, err := io.Copy(part, file); err != nil {
		return err
	}

	if len(args) > 1 {
		writer.WriteField("path", args[1])
	}

	writer.Close()

	// Upload
	client := getClient(cmd)
	req, err := http.NewRequest("POST", client.baseURL+"/api/v1/upload", &buf)
	if err != nil {
		return err
	}

	if client.token != "" {
		req.Header.Set("Authorization", client.token)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload failed: %v", result["error"])
	}

	fmt.Printf("File uploaded successfully\n")
	fmt.Printf("ID: %s\n", result["id"])
	fmt.Printf("Path: %s\n", result["path"])

	return nil
}

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <workflow.cwl>",
		Short: "Validate a CWL document",
		Args:  cobra.ExactArgs(1),
		RunE:  runValidate,
	}
}

func runValidate(cmd *cobra.Command, args []string) error {
	workflowPath := args[0]

	// Read and parse workflow
	workflowData, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("failed to read workflow: %w", err)
	}

	var workflow interface{}
	if filepath.Ext(workflowPath) == ".json" {
		if err := json.Unmarshal(workflowData, &workflow); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(workflowData, &workflow); err != nil {
			return fmt.Errorf("failed to parse YAML: %w", err)
		}
	}

	// Validate
	reqBody, _ := json.Marshal(map[string]interface{}{"document": workflow})
	client := getClient(cmd)

	resp, err := client.doRequest("POST", "/api/v1/validate", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Valid    bool     `json:"valid"`
		Errors   []string `json:"errors"`
		Warnings []string `json:"warnings"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if result.Valid {
		fmt.Println("Workflow is valid")
	} else {
		fmt.Println("Workflow validation failed:")
		for _, e := range result.Errors {
			fmt.Printf("  ERROR: %s\n", e)
		}
	}

	for _, w := range result.Warnings {
		fmt.Printf("  WARNING: %s\n", w)
	}

	if !result.Valid {
		os.Exit(1)
	}

	return nil
}

func newOutputsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "outputs <workflow-id>",
		Short: "Get workflow outputs",
		Args:  cobra.ExactArgs(1),
		RunE:  runOutputs,
	}
}

func runOutputs(cmd *cobra.Command, args []string) error {
	id := args[0]
	client := getClient(cmd)

	resp, err := client.doRequest("GET", "/api/v1/workflows/"+id+"/outputs", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		return fmt.Errorf("failed to get outputs: %v", result["error"])
	}

	var outputs map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&outputs); err != nil {
		return err
	}

	outputJSON, _ := json.MarshalIndent(outputs, "", "  ")
	fmt.Println(string(outputJSON))

	return nil
}

func newStepsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "steps <workflow-id>",
		Short: "Get workflow step statuses",
		Args:  cobra.ExactArgs(1),
		RunE:  runSteps,
	}
}

func runSteps(cmd *cobra.Command, args []string) error {
	id := args[0]
	client := getClient(cmd)

	resp, err := client.doRequest("GET", "/api/v1/workflows/"+id+"/steps", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var steps []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&steps); err != nil {
		return err
	}

	if len(steps) == 0 {
		fmt.Println("No steps found")
		return nil
	}

	fmt.Printf("%-20s  %-12s  %-12s\n", "STEP", "STATUS", "TASK ID")
	for _, s := range steps {
		stepID := s["step_id"].(string)
		status := s["status"].(string)
		taskID := ""
		if tid, ok := s["bvbrc_task_id"].(float64); ok && tid > 0 {
			taskID = fmt.Sprintf("%.0f", tid)
		}
		fmt.Printf("%-20s  %-12s  %-12s\n", stepID, status, taskID)
	}

	return nil
}
