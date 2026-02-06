# BV-BRC App Service API Reference

This document describes the BV-BRC App Service JSON-RPC 2.0 API used by the CWL workflow system.

## Overview

The App Service provides task submission and management for BV-BRC applications running on SLURM clusters. It uses JSON-RPC 2.0 over HTTP.

**Base URL:** `https://p3.theseed.org/services/app_service`

**Authentication:** All requests require a P3 authentication token in the `Authorization` header.

## JSON-RPC Request Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "AppService.<method_name>",
  "params": [<param1>, <param2>, ...]
}
```

**Headers:**
```
Content-Type: application/json
Authorization: <p3_token>
```

---

## Data Types

### Task

```typescript
interface Task {
  id: string;              // Task ID
  parent_id?: string;      // Parent task ID (for linked tasks)
  app: string;             // Application ID
  workspace?: string;      // Workspace context
  parameters: Record<string, string>;  // Task parameters
  user_id: string;         // Owner
  status: TaskStatus;      // Current status
  awe_status?: string;     // Legacy AWE status
  submit_time: string;     // ISO timestamp
  start_time?: string;     // ISO timestamp
  completed_time?: string; // ISO timestamp
  elapsed_time?: string;   // Duration string
  stdout_shock_node?: string;
  stderr_shock_node?: string;
}
```

### TaskStatus

Valid status values:
- `queued` - Task is waiting to be scheduled
- `submitted` - Task submitted to SLURM
- `in-progress` / `running` - Task is executing
- `completed` - Task finished successfully
- `failed` - Task failed
- `deleted` / `cancelled` - Task was cancelled

### StartParams

Extended parameters for `start_app2`:

```typescript
interface StartParams {
  parent_id?: string;      // Link to parent task
  workspace?: string;      // Workspace context
  base_url?: string;       // BV-BRC web URL (e.g., "https://www.bv-brc.org")
  container_id?: string;   // Container image override
  user_metadata?: string;  // Custom metadata JSON
  reservation?: string;    // SLURM reservation name
  data_container_id?: string; // Data staging container
  disable_preflight?: number; // Skip preflight checks (0 or 1)
  preflight_data?: Record<string, string>; // Pre-computed preflight data
}
```

### App

Application definition:

```typescript
interface App {
  id: string;              // Application ID
  script?: string;         // Entry script
  label: string;           // Display name
  description?: string;    // Description
  parameters: AppParameter[];
}

interface AppParameter {
  id: string;
  label?: string;
  required: number;        // 0 = optional, 1 = required
  default?: string;
  desc?: string;
  type: string;            // "string", "int", "float", "bool", "enum", "wsid", "folder"
  enum?: string;           // Comma-separated enum values
  wstype?: string;         // Workspace object type
}
```

---

## Methods

### start_app

Submit a task using basic parameters.

**Signature:**
```
start_app(app_id, params, workspace) → Task
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `app_id` | string | Application ID (e.g., "GenomeAssembly2") |
| `params` | Record<string, string> | Task parameters (all values are strings) |
| `workspace` | string | Workspace path for outputs |

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "AppService.start_app",
  "params": [
    "GenomeAssembly2",
    {
      "contigs": "/user@patricbrc.org/home/contigs.fa",
      "output_path": "/user@patricbrc.org/home/results",
      "output_file": "assembly"
    },
    "/user@patricbrc.org/home"
  ]
}
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "12345",
    "app": "GenomeAssembly2",
    "workspace": "/user@patricbrc.org/home",
    "parameters": {...},
    "user_id": "user@patricbrc.org",
    "status": "queued",
    "submit_time": "2026-02-06T20:00:00"
  }
}
```

---

### start_app2

Submit a task with extended parameters. **Recommended for CWL integration.**

**Signature:**
```
start_app2(app_id, params, start_params) → Task
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `app_id` | string | Application ID |
| `params` | Record<string, string> | Task parameters |
| `start_params` | StartParams | Extended submission options |

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "AppService.start_app2",
  "params": [
    "CWLRunner",
    {
      "cwl_job_spec": "{\"tool\": {...}, \"inputs\": {...}}",
      "output_path": "/user@patricbrc.org/home/results/run-123"
    },
    {
      "base_url": "https://www.bv-brc.org",
      "container_id": "biocontainers/bwa:0.7.17",
      "parent_id": "workflow-abc123"
    }
  ]
}
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "12346",
    "parent_id": "workflow-abc123",
    "app": "CWLRunner",
    "parameters": {...},
    "user_id": "user@patricbrc.org",
    "status": "queued",
    "submit_time": "2026-02-06T20:00:00"
  }
}
```

**Key Differences from start_app:**
- `start_params.container_id` - Override the application's default container
- `start_params.parent_id` - Link tasks for workflow tracking
- `start_params.reservation` - Use specific SLURM reservation
- `start_params.disable_preflight` - Skip resource validation

---

### query_tasks

Query status of specific tasks.

**Signature:**
```
query_tasks(task_ids) → Record<task_id, Task>
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `task_ids` | string[] | Array of task IDs to query |

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "AppService.query_tasks",
  "params": [["12345", "12346"]]
}
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "12345": {
      "id": "12345",
      "app": "CWLRunner",
      "status": "completed",
      "submit_time": "2026-02-06T20:00:00",
      "start_time": "2026-02-06T20:01:00",
      "completed_time": "2026-02-06T20:15:00",
      "elapsed_time": "00:14:00"
    },
    "12346": {
      "id": "12346",
      "app": "CWLRunner",
      "status": "in-progress",
      "submit_time": "2026-02-06T20:00:00",
      "start_time": "2026-02-06T20:02:00"
    }
  }
}
```

---

### query_task_summary

Get count of tasks by status for the authenticated user.

**Signature:**
```
query_task_summary() → Record<TaskStatus, number>
```

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "AppService.query_task_summary",
  "params": []
}
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "queued": 2,
    "in-progress": 5,
    "completed": 142,
    "failed": 3
  }
}
```

---

### query_task_details

Get detailed execution information for a task.

**Signature:**
```
query_task_details(task_id) → TaskDetails
```

**Returns:**
```typescript
interface TaskDetails {
  stdout_url?: string;   // URL to stdout content
  stderr_url?: string;   // URL to stderr content
  pid?: number;          // Process ID on compute node
  hostname?: string;     // Compute node hostname
  exitcode?: number;     // Exit code (if completed)
}
```

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "AppService.query_task_details",
  "params": ["12345"]
}
```

---

### enumerate_tasks

List tasks with pagination.

**Signature:**
```
enumerate_tasks(offset, count) → Task[]
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `offset` | number | Starting offset (0-based) |
| `count` | number | Maximum tasks to return |

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "AppService.enumerate_tasks",
  "params": [0, 50]
}
```

---

### enumerate_tasks_filtered

List tasks with filtering and pagination.

**Signature:**
```
enumerate_tasks_filtered(offset, count, filter) → {tasks: Task[], total_tasks: number}
```

**Filter:**
```typescript
interface SimpleTaskFilter {
  start_time?: string;   // ISO timestamp (tasks after)
  end_time?: string;     // ISO timestamp (tasks before)
  app?: string;          // Filter by app ID
  search?: string;       // Text search
  status?: string;       // Filter by status
}
```

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "AppService.enumerate_tasks_filtered",
  "params": [
    0,
    50,
    {
      "app": "CWLRunner",
      "status": "completed"
    }
  ]
}
```

---

### kill_task

Cancel a single task.

**Signature:**
```
kill_task(task_id) → {killed: number, msg: string}
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `task_id` | string | Task ID to cancel |

**Returns:**
- `killed`: 1 if killed, 0 if not
- `msg`: Status message

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "AppService.kill_task",
  "params": ["12345"]
}
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "result": [1, "Task 12345 terminated"]
}
```

---

### kill_tasks

Cancel multiple tasks.

**Signature:**
```
kill_tasks(task_ids) → Record<task_id, {killed: number, msg: string}>
```

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 8,
  "method": "AppService.kill_tasks",
  "params": [["12345", "12346", "12347"]]
}
```

---

### rerun_task

Resubmit a failed task.

**Signature:**
```
rerun_task(task_id) → Task
```

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 9,
  "method": "AppService.rerun_task",
  "params": ["12345"]
}
```

---

### enumerate_apps

List all available applications.

**Signature:**
```
enumerate_apps() → App[]
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "result": [
    {
      "id": "GenomeAssembly2",
      "label": "Genome Assembly",
      "description": "Assemble reads into contigs",
      "parameters": [...]
    },
    {
      "id": "CWLRunner",
      "label": "CWL Workflow Step",
      "description": "Execute a CWL CommandLineTool",
      "parameters": [...]
    }
  ]
}
```

---

### service_status

Check if the service is accepting submissions.

**Signature:**
```
service_status() → {submission_enabled: number, status_message: string}
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "result": [1, "Service operational"]
}
```

---

## Error Handling

**Error Response Format:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32000,
    "message": "Error description",
    "data": "Additional details"
  }
}
```

**Common Error Codes:**
| Code | Meaning |
|------|---------|
| -32700 | Parse error (invalid JSON) |
| -32600 | Invalid request |
| -32601 | Method not found |
| -32602 | Invalid params |
| -32603 | Internal error |
| -32000 | Application error |

---

## CWL Integration Usage

For the CWL workflow system, use `start_app2` to submit jobs:

```go
// Go client example
func (e *BVBRCExecutor) SubmitJob(ctx context.Context, token string, jobSpec *bvbrc.CWLJobSpec) (string, error) {
    params := map[string]string{
        "cwl_job_spec": string(jobSpec.ToJSON()),
        "output_path":  jobSpec.OutputPath,
    }

    startParams := StartParams{
        BaseURL:     "https://www.bv-brc.org",
        ContainerID: jobSpec.GetContainerID(),
        ParentID:    workflowRunID, // Link to parent workflow
    }

    task, err := client.StartApp2(ctx, token, "CWLRunner", params, startParams)
    return task.ID, err
}
```

**Polling for Completion:**
```go
func (e *BVBRCExecutor) WaitForCompletion(ctx context.Context, token, taskID string) error {
    for {
        tasks, err := client.QueryTasks(ctx, token, []string{taskID})
        if err != nil {
            return err
        }

        task := tasks[taskID]
        switch task.Status {
        case "completed":
            return nil
        case "failed":
            return fmt.Errorf("task failed")
        case "deleted", "cancelled":
            return fmt.Errorf("task cancelled")
        }

        time.Sleep(10 * time.Second)
    }
}
```

---

## Source Reference

API defined in:
- `app_service/AppService.spec` - Type definitions
- `app_service/lib/Bio/KBase/AppService/AppServiceImpl.pm` - Implementation
- `app_service/lib/Bio/KBase/AppService/SchedulerDB.pm` - Database layer
