# CWL Workflow System for BV-BRC

## Overview

A Go-based CWL (Common Workflow Language) v1.2 workflow execution system that integrates with BV-BRC's existing SLURM infrastructure. Each CWL workflow step is submitted as a BV-BRC Task via app_service, leveraging the proven scheduling and container execution infrastructure.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                CWL Workflow Service (Go)                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  REST API   │  │ CWL Parser  │  │  DAG Scheduler      │  │
│  │  (Chi/Gin)  │  │  (v1.2)     │  │  (Dependency Mgmt)  │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ BV-BRC      │  │ State Store │  │  Event Processor    │  │
│  │ Executor    │  │ (MongoDB)  │  │  (Redis PubSub)     │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└───────────────────────────────┬─────────────────────────────┘
                                │
                ┌───────────────┼───────────────┐
                ▼               ▼               ▼
         ┌──────────┐    ┌──────────┐    ┌─────────────┐
         │ MongoDB  │    │  Redis   │    │ BV-BRC      │
         │(CWL State)│    │(Events)  │    │ app_service │
         └──────────┘    └──────────┘    └──────┬──────┘
                                                │
                                                ▼
                                         ┌─────────────┐
                                         │   SLURM     │
                                         │  Cluster    │
                                         └─────────────┘
```

## Go Package Structure

```
cwe-cwl/
├── cmd/
│   ├── cwe-server/           # Main API server
│   ├── cwe-scheduler/        # Background scheduler daemon
│   └── cwe-cli/              # CLI: uploads files to Workspace/Shock, submits workflow
├── internal/
│   ├── api/                  # REST handlers, routes, middleware
│   ├── cwl/                  # CWL parsing (parser, types, workflow, commandtool, expression, scatter)
│   ├── dag/                  # DAG construction and scheduling
│   ├── executor/             # Step execution (bvbrc bridge, local dev mode)
│   ├── state/                # MongoDB state management
│   ├── staging/              # File staging (workspace, local)
│   ├── events/               # Redis pub/sub handlers
│   └── config/               # Configuration
├── pkg/
│   ├── auth/                 # P3 token validation
│   └── client/               # API client library
└── configs/                  # Configuration files
```

## Repository

GitHub repository: `github.com/wilke/cwe-cwl`

## Database Schema

**MongoDB** (same as AWE, flexible for CWL document storage):

```javascript
// Collection: workflows (cached CWL documents)
{
  _id: ObjectId,
  workflow_id: "my-workflow",
  content_hash: "sha256:...",
  cwl_version: "v1.2",
  document: { /* full CWL document */ },
  created_at: ISODate
}

// Collection: workflow_runs
{
  _id: "wf-uuid",
  workflow_id: "my-workflow",
  owner: "user@patricbrc.org",
  status: "running",  // pending/running/completed/failed/cancelled
  inputs: { /* resolved inputs */ },
  outputs: { /* collected outputs */ },
  output_path: "/user@patricbrc.org/home/results/",
  dag_state: { /* serialized DAG for recovery */ },
  error_message: null,
  created_at: ISODate,
  started_at: ISODate,
  completed_at: ISODate
}

// Collection: step_executions (links to BV-BRC Tasks)
{
  _id: ObjectId,
  workflow_run_id: "wf-uuid",
  step_id: "align",
  scatter_index: [0],  // for scattered steps
  status: "completed",
  bvbrc_task_id: 12345,  // FK to BV-BRC Task table
  inputs: { /* step inputs */ },
  outputs: { /* collected outputs */ },
  error_message: null,
  created_at: ISODate,
  started_at: ISODate,
  completed_at: ISODate,
  retry_count: 0
}

// Collection: container_mappings
{
  _id: "docker.io/biocontainers/bwa:0.7.17",
  bvbrc_container_id: "bwa-0.7.17",
  verified: true,
  created_at: ISODate
}
```

## REST API

**Design rationale**: Simpler than AWE's API because BV-BRC handles worker/cluster management. No `/work` (worker polling) or `/cgroup` (client groups) endpoints needed.

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/workflows` | Submit workflow (CWL doc + job file with file refs) |
| GET | `/workflows` | List user's workflows |
| GET | `/workflows/{id}` | Get workflow status |
| GET | `/workflows/{id}/steps` | Get all step statuses |
| GET | `/workflows/{id}/outputs` | Get workflow output paths (in Workspace/Shock) |
| DELETE | `/workflows/{id}` | Cancel workflow |
| POST | `/workflows/{id}/rerun` | Rerun failed workflow |
| POST | `/validate` | Validate CWL document |
| POST | `/validate-inputs` | Validate job file (check file accessibility) |
| POST | `/upload` | Upload file to server local storage (for users without backend access) |
| GET | `/files/{id}` | Download cached file from server |

**Admin REST API:**

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/admin/workflows` | List workflows across users |
| GET | `/admin/workflows/{id}` | Get workflow status across users |
| DELETE | `/admin/workflows/{id}` | Cancel workflow across users |
| POST | `/admin/workflows/{id}/rerun` | Rerun workflow across users |
| GET | `/admin/workflows/{id}/steps` | List steps across users |
| POST | `/admin/workflows/{id}/steps/{step_id}/requeue` | Requeue a step (optional `scatter_index=0,1`) |

**Storage modes**: Files can be in Workspace, Shock, or server local storage. Local storage enables users without direct backend permissions.

## Authentication Model (Hybrid)

```
┌─────────────┐     User Token      ┌─────────────────┐
│    User     │ ─────────────────── │   CWL Service   │
│   (CLI)     │                     │                 │
└─────────────┘                     └────────┬────────┘
                                             │
                    ┌────────────────────────┼────────────────────────┐
                    │                        │                        │
                    ▼                        ▼                        ▼
           ┌───────────────┐        ┌───────────────┐        ┌───────────────┐
           │   Workspace   │        │   app_service │        │    MongoDB    │
           │  (User Token) │        │(Service Token)│        │ (Internal)    │
           └───────────────┘        └───────────────┘        └───────────────┘
```

**Authentication Flow:**
1. **User Token** (passed in request header):
   - Used to validate Workspace file accessibility
   - Used to verify user identity
   - Checked for permissions before workflow starts

2. **Service Token** (configured in server):
   - Used for BV-BRC task submission
   - Used for task status queries
   - Allows service to act on behalf of users

3. **User Identity Preserved**:
   - `owner` field in MongoDB workflow_run documents
   - `owner` field in BV-BRC Task records
   - Enables per-user quota enforcement and auditing

**Configuration:**
```yaml
auth:
  service_token: "eyJ..."  # Service account P3 token
  validate_user_tokens: true
  workspace_url: "https://p3.theseed.org/services/Workspace"
  admin_users: ["admin@patricbrc.org"]
```

## CLI Workflow (cwe-cli)

```bash
# 1. User uploads input files to Workspace (using existing BV-BRC CLI or cwe-cli)
cwe-cli upload reads.fastq /user@patricbrc.org/home/data/

# 2. User creates job file with Workspace paths
cat job.yaml
input_reads:
  class: File
  path: /user@patricbrc.org/home/data/reads.fastq
reference:
  class: File
  path: /user@patricbrc.org/home/refs/genome.fasta

# 3. Submit workflow with job file
cwe-cli submit workflow.cwl job.yaml --output /user@patricbrc.org/home/results/

# 4. Monitor status
cwe-cli status wf-abc123

# 5. Outputs appear in Workspace at specified output path
```

## Data Storage Architecture

**Three storage backends supported:**

| Backend | Use Case | Path Format |
|---------|----------|-------------|
| **Local** | Fast caching, users without direct backend access | `/data/uploads/abc123.fastq` |
| **Workspace** | BV-BRC native storage, user permissions | `/user@patricbrc.org/home/reads.fastq` |
| **Shock** | Large scientific datasets, node-based | `shock://host/node/abc123` |

**Data Flow Options:**

```
Option A: Direct Backend Access (user has permissions)
┌──────┐    upload    ┌─────────────────┐    reference    ┌─────────────┐
│ CLI  │ ───────────► │ Workspace/Shock │ ◄────────────── │ CWL Service │
└──────┘              └─────────────────┘                 └─────────────┘

Option B: Server-Mediated (user lacks direct backend access)
┌──────┐    upload    ┌─────────────┐    stage     ┌─────────────────┐
│ CLI  │ ───────────► │ CWL Server  │ ───────────► │ Workspace/Shock │
└──────┘    (local)   │ (local FS)  │  (service)   └─────────────────┘
                      └─────────────┘
```

**Local caching**: Server can cache frequently-used files (references, containers) on local filesystem for performance.

## Workflow Execution Flow

1. **CLI Upload**: User uploads input files to Workspace/Shock via CLI
2. **Submit**: CLI POSTs workflow document + job file (with file references) to server
3. **Validate**: Server checks that all referenced files are accessible in Workspace/Shock
4. **Parse**: Validate CWL, resolve imports, type-check inputs against file metadata
5. **Store**: Create `workflow_run` record in MongoDB (status: pending)
6. **Build DAG**: Analyze step dependencies, handle scatter
7. **Schedule Loop**:
   - Find ready steps (all dependencies satisfied)
   - For each ready step:
     - Resolve container (Docker → BV-BRC mapping)
     - Build command line from CWL tool definition
     - Submit BV-BRC Task via app_service API using `CWLStepRunner`
     - Task params include Workspace/Shock paths for staging
     - Receive `task_id` from app_service response
8. **Monitor**: Subscribe to Redis `task_completion` channel when available, otherwise poll app_service task status
9. **Complete**: When task completes, outputs are in Workspace; update DAG, schedule next steps
10. **Finish**: When all steps done, collect workflow output paths, update status

## BV-BRC Integration

### CWLStepRunner Application

New BV-BRC application that executes arbitrary CWL CommandLineTools:

```perl
# App-CWLStepRunner.pl
# Params: cwl_command, cwl_inputs, cwl_outputs, cwl_work_dir
# - Executes command in container
# - Collects outputs per CWL output bindings
# - Writes cwl_outputs.json for collection
```

### Task Creation (Go → BV-BRC)

```go
// Submit task via BV-BRC app_service API
// Response returns a BV-BRC task_id
POST {app_service_url}/tasks
Authorization: <user or service token>
{
  application_id: "CWLStepRunner",
  params: {...},
  req_cpu: ...,
  req_memory: ...,
  req_runtime: ...,
  container_id: ...,
  output_path: ...,
  output_file: ...
}
```

If Redis events are emitted by BV-BRC, subscribe to `task_completion` for completion signals.

### app_service API Contract (Proposed)

Base URL: `bvbrc.app_service_url`

Headers:
`Authorization: <user-or-service-token>`
`Content-Type: application/json`
`Accept: application/json`

Submit task:
`POST /tasks`
Request:
```json
{
  "application_id": "CWLStepRunner",
  "params": {
    "cwl_command": ["bwa", "mem", "..."],
    "cwl_inputs": {},
    "cwl_outputs": {},
    "cwl_step_id": "align",
    "cwl_node_id": "align#scatter-0"
  },
  "req_cpu": 4,
  "req_memory": 16384,
  "req_runtime": 86400,
  "container_id": "bwa-0.7.17",
  "output_path": "/user@patricbrc.org/home/results/run-123",
  "output_file": "cwl_outputs.json",
  "owner": "user@patricbrc.org"
}
```
Response:
```json
{
  "task_id": 12345,
  "state_code": "Q",
  "status": "queued",
  "submit_time": "2026-02-06T19:41:00Z"
}
```

Task status:
`GET /tasks/{task_id}`
Response:
```json
{
  "task_id": 12345,
  "state_code": "R",
  "status": "running",
  "owner": "user@patricbrc.org",
  "submit_time": "2026-02-06T19:41:00Z",
  "start_time": "2026-02-06T19:42:30Z",
  "end_time": null,
  "exit_code": null,
  "error": null
}
```

Cancel task:
`POST /tasks/{task_id}/cancel`
Response:
```json
{
  "task_id": 12345,
  "state_code": "K",
  "status": "cancelled",
  "cancel_time": "2026-02-06T19:50:10Z"
}
```

Task outputs:
`GET /tasks/{task_id}/outputs`
Response:
```json
{
  "task_id": 12345,
  "output_path": "/user@patricbrc.org/home/results/run-123",
  "output_file": "cwl_outputs.json",
  "outputs": {}
}
```

Error response (all endpoints):
```json
{
  "error": "message",
  "code": "ERR_CODE",
  "request_id": "req-uuid"
}
```

## Key Patterns from AWE (Adapted for BV-BRC)

- **Push-based submission**: CWL service pushes tasks via app_service; BV-BRC Scheduler submits to SLURM via `sbatch` (differs from AWE's pull model since we use existing SLURM infrastructure)
- **Dual persistence**: MongoDB for queries + BSON/JSON files for recovery (like AWE)
- **Event-driven monitoring**: Redis pub/sub for `task_completion` events from BV-BRC
- **Pipeline stages**: Stage data → Execute in SLURM → Collect outputs
- **Job hierarchy**: Workflow → Steps → Tasks (maps to CWL Workflow → Steps → BV-BRC Tasks)

## CWL v1.2 Feature Support

| Feature | Priority | Notes |
|---------|----------|-------|
| CommandLineTool | P0 | Core execution unit |
| Workflow | P0 | Step orchestration |
| ResourceRequirement | P0 | Maps to Task req_cpu/req_memory |
| DockerRequirement | P0 | Maps to container_id |
| ApptainerRequirement | P0 | BV-BRC extension for HPC |
| CUDARequirement | P0 | GPU support (cwltool extension) |
| Scatter/ScatterMethod | P1 | Parallel step execution |
| SubworkflowFeatureRequirement | P1 | Nested workflows |
| InlineJavascriptRequirement | P1 | Expression evaluation (use goja) |
| Conditional (when) | P2 | Skip steps based on condition |
| ExpressionTool | P2 | Pure JS computation |

## Implementation Phases

### Phase 1: Core Infrastructure (Weeks 1-3)
- Go project scaffolding in new `github.com/BV-BRC/cwe-cwl` repo
- MongoDB collections and indexes
- CWL parser for CommandLineTool and Workflow
- Basic REST API (submit, status, list)
- DAG construction and topological sort

### Phase 2: BV-BRC Integration (Weeks 4-6)
- AppServiceExecutor: Task creation via BV-BRC app_service API
- Redis event subscription for task_completion
- CWLStepRunner Perl application
- Container resolution and mapping
- End-to-end single-step workflow test

### Phase 3: File Validation & Output Handling (Weeks 7-8)
- Workspace/Shock file accessibility validation (check files exist, user has access)
- Step execution passes file references to CWLStepRunner (staging done in container)
- Output path resolution and metadata collection from Workspace
- Working directory management within SLURM jobs

### Phase 4: Advanced Features (Weeks 9-11)
- Scatter/gather implementation
- JavaScript expression evaluation (goja library)
- Subworkflow support
- Conditional execution

### Phase 5: Production Hardening (Weeks 12-14)
- Error recovery and retry logic
- Workflow resumption after failure
- Performance optimization
- Monitoring, logging, alerting
- Documentation

## Critical Files to Reference

| File | Purpose |
|------|---------|
| `app_service/lib/Bio/KBase/AppService/SlurmCluster.pm` | Task submission patterns, container handling |
| `app_service/lib/Bio/KBase/AppService/Schema.sql` | Database schema for Tasks, ClusterJobs |
| `app_service/lib/Bio/KBase/AppService/Scheduler.pm` | Redis pub/sub, queue management |
| `app_service/lib/Bio/KBase/AppService/slurm_batch.tt` | SLURM batch script template |
| `app_service/lib/Bio/KBase/AppService/SchedulerDB.pm` | Direct DB access patterns |

## Verification Plan

1. **Unit Tests**: CWL parser, DAG scheduler, type validation
2. **Integration Tests**:
   - Submit single-step workflow → verify BV-BRC Task created
   - Task completion → verify outputs collected
   - Multi-step workflow → verify dependency ordering
3. **End-to-End Tests**:
   - Real CWL workflow (e.g., simple bioinformatics pipeline)
   - Submit via REST API
   - Verify execution on SLURM cluster
   - Verify outputs in BV-BRC Workspace
4. **Load Tests**: Multiple concurrent workflows with scattered steps

## Dependencies

- Go 1.21+
- MongoDB 6+
- Redis 6+
- Libraries: `github.com/go-chi/chi`, `go.mongodb.org/mongo-driver`, `github.com/redis/go-redis`, `github.com/dop251/goja` (JS engine)

## Security Considerations

### Threat Model

The CWL workflow system processes user-submitted workflow definitions that specify:
- Arbitrary commands to execute in containers
- JavaScript expressions evaluated on the server
- File paths for input/output staging
- Container images to pull and run

This creates a significant attack surface requiring defense-in-depth.

### Input Validation

| Input | Validation | Limit |
|-------|------------|-------|
| CWL document | Schema validation against CWL v1.2 spec | Max 1MB |
| Job file (inputs) | Type checking, path validation | Max 10MB |
| Workflow ID | UUID format only | — |
| File paths | No `..`, must be within allowed roots | — |
| Scatter arrays | Cardinality limit | Max 1,000 items |
| Request body | Size limit on all endpoints | Max 10MB |

**Path Traversal Prevention:**
```go
// All file paths must be canonicalized and validated
func validatePath(path string, allowedRoots []string) error {
    clean := filepath.Clean(path)
    if strings.Contains(clean, "..") {
        return ErrPathTraversal
    }
    for _, root := range allowedRoots {
        if strings.HasPrefix(clean, root) {
            return nil
        }
    }
    return ErrPathOutsideAllowedRoot
}
```

### Container Security

**Image Allowlist:**

Only permit container images from approved registries:

```yaml
container:
  allowed_registries:
    - "docker.io/biocontainers/"
    - "ghcr.io/bv-brc/"
    - "quay.io/biocontainers/"
  block_latest_tag: true  # Require explicit version tags
```

**Runtime Restrictions:**

| Runtime | Security Flags |
|---------|---------------|
| Docker | `--rm`, `--read-only` (where possible), `--network=none` (if no network needed) |
| Podman | `--rm`, rootless mode preferred, `:Z` SELinux labels |
| Apptainer | `--containall`, `--cleanenv`, no `--writable` |

**GPU Passthrough Risks:**
- `--nv` (NVIDIA) and `--device` flags increase attack surface
- GPU workloads should run in dedicated, isolated queues
- Monitor for cryptomining indicators

### JavaScript Expression Sandboxing

The goja JavaScript engine executes user-supplied expressions. Required safeguards:

```go
// Expression execution constraints
type ExpressionLimits struct {
    Timeout       time.Duration // Max 5 seconds
    MaxMemory     int64         // Max 50MB
    MaxStackDepth int           // Max 100
}

// Disabled/restricted APIs in sandbox
var disabledGlobals = []string{
    "require",    // No module loading
    "process",    // No process access
    "eval",       // No dynamic code execution
    "Function",   // No function constructor
}
```

**Loop Protection:**
- Interrupt long-running scripts via context cancellation
- Detect infinite loops via instruction counting

### Authentication & Authorization

**Token Security:**

| Token Type | Storage | Rotation | Scope |
|------------|---------|----------|-------|
| User Token | Client-side, passed in header | Per-session | User's own resources |
| Service Token | Secrets manager (Vault/K8s) | 90 days | Task submission on behalf of users |

**Never store service tokens in:**
- Config files committed to git
- Environment variables in Dockerfiles
- Log output

**Authorization Checks:**

```go
// Every resource access must validate ownership
func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
    workflowID := chi.URLParam(r, "id")
    userID := auth.GetUserID(r.Context())

    workflow, err := h.store.GetWorkflow(workflowID)
    if err != nil {
        // Don't reveal whether resource exists
        http.Error(w, "not found", http.StatusNotFound)
        return
    }

    // CRITICAL: Verify ownership
    if workflow.Owner != userID && !auth.IsAdmin(r.Context()) {
        http.Error(w, "not found", http.StatusNotFound)
        return
    }
    // ... return workflow
}
```

**Admin Access:**
- Admin users defined in config (via secrets manager)
- All admin actions logged with full context
- Consider requiring MFA for admin operations

### Network Security

| Communication | Requirement |
|---------------|-------------|
| Client → API Server | TLS 1.2+ required |
| API Server → MongoDB | TLS + authentication |
| API Server → Redis | TLS + AUTH password |
| API Server → app_service | TLS + service token |
| API Server → Workspace/Shock | TLS + user token |

**SSRF Prevention:**
- Validate container image URIs against allowlist
- Block requests to internal IP ranges (10.x, 172.16.x, 192.168.x, 169.254.x)
- Use dedicated egress proxy for external calls

### Secrets Management

**Required Secrets:**

| Secret | Source | Never In |
|--------|--------|----------|
| `service_token` | Vault / K8s Secret | Config files, env vars in code |
| `mongodb.password` | Vault / K8s Secret | Config files |
| `redis.password` | Vault / K8s Secret | Config files |

**Configuration Pattern:**
```yaml
# config.yaml - safe to commit
auth:
  service_token: "${CWE_SERVICE_TOKEN}"  # Injected at runtime

# In production, use:
# - Kubernetes Secrets mounted as files
# - HashiCorp Vault with dynamic credentials
# - AWS Secrets Manager / GCP Secret Manager
```

### Audit Logging

**Required Audit Events:**

| Event | Fields |
|-------|--------|
| Workflow submitted | user, workflow_id, timestamp, source_ip, cwl_hash |
| Workflow status change | workflow_id, old_status, new_status, timestamp |
| Task submitted | workflow_id, step_id, task_id, container_image |
| Task completed | task_id, exit_code, duration |
| File accessed | user, file_path, operation (read/write), timestamp |
| Authentication failure | source_ip, attempted_user, reason, timestamp |
| Admin action | admin_user, action, target_resource, timestamp |

**Log Format (structured JSON):**
```json
{
  "timestamp": "2026-02-06T19:41:00Z",
  "level": "info",
  "event": "workflow.submitted",
  "user": "user@patricbrc.org",
  "source_ip": "192.0.2.1",
  "workflow_id": "wf-abc123",
  "cwl_hash": "sha256:...",
  "request_id": "req-xyz789"
}
```

**Retention:** Minimum 90 days, or per compliance requirements.

### Rate Limiting

| Endpoint Category | Limit | Burst |
|-------------------|-------|-------|
| Workflow submission | 10/minute/user | 5 |
| Status queries | 100/minute/user | 20 |
| File upload | 5/minute/user | 2 |
| Validation | 20/minute/user | 10 |
| Admin endpoints | 50/minute/admin | 10 |

**Implementation:** Use token bucket algorithm with Redis backend for distributed rate limiting.

### Resource Quotas

**Per-User Limits:**

| Resource | Default Limit | Configurable |
|----------|---------------|--------------|
| Concurrent workflows | 10 | Yes |
| Total CPU (active) | 100 cores | Yes |
| Total memory (active) | 500 GB | Yes |
| Storage (Workspace) | 1 TB | Via Workspace |
| Scatter cardinality | 1,000 | Yes |

**System-Wide Limits:**

| Resource | Limit |
|----------|-------|
| Max concurrent tasks | 10,000 |
| Max workflow DAG depth | 100 |
| Max steps per workflow | 500 |

### Denial of Service Prevention

**Scatter Bombs:**
- User submits scatter with 1M items → creates 1M tasks
- Mitigation: Hard limit on scatter cardinality (default 1,000)
- Validate total task count before DAG execution begins

**Expression Bombs:**
- Malicious JS: `while(true){}` or exponential string growth
- Mitigation: Timeout (5s), memory limit (50MB), instruction counting

**Large File Uploads:**
- Mitigation: Request body size limits, streaming uploads with progress tracking
- Abort uploads exceeding quota

**Recursive Workflows:**
- Workflow A calls Workflow B calls Workflow A
- Mitigation: Track call stack, max depth limit (10)

### Incident Response

**Security Event Classification:**

| Severity | Examples | Response Time |
|----------|----------|---------------|
| Critical | Service token compromised, container escape | Immediate |
| High | Successful auth bypass, data exfiltration attempt | < 1 hour |
| Medium | Repeated auth failures, quota bypass | < 4 hours |
| Low | Invalid input attempts, minor policy violations | < 24 hours |

**Runbooks Required:**
1. Service token rotation procedure
2. User account suspension
3. Workflow kill-all for user
4. Container image blocklist update
5. Forensic log collection

### Security Testing

**Required Before Production:**

| Test Type | Scope | Frequency |
|-----------|-------|-----------|
| SAST (static analysis) | All Go code | Every PR |
| Dependency scanning | go.mod, container images | Daily |
| DAST (dynamic) | REST API | Weekly |
| Penetration test | Full system | Annually |
| Container image scan | All approved images | On push |

**Security-Focused Test Cases:**
- [ ] Path traversal in file staging
- [ ] IDOR on workflow/file access
- [ ] JS expression timeout enforcement
- [ ] Container image allowlist enforcement
- [ ] SQL/NoSQL injection in queries
- [ ] Rate limit bypass attempts
- [ ] Token validation edge cases

### Compliance Considerations

If processing sensitive data (PHI, PII, controlled research data):

| Requirement | Implementation |
|-------------|----------------|
| Data encryption at rest | MongoDB encryption, encrypted volumes |
| Data encryption in transit | TLS everywhere |
| Access logging | Audit log all data access |
| Data retention | Configurable, auto-deletion |
| Right to deletion | Workflow purge API |

---

## Implementation Progress

**Last Updated:** 2026-02-06

### Overall Status

| Phase | Description | Progress | Status |
|-------|-------------|----------|--------|
| Phase 1 | Core Infrastructure | 90% | ✅ Nearly Complete |
| Phase 2 | BV-BRC Integration | 40% | 🟡 In Progress |
| Phase 3 | File Validation & Output | 30% | 🟡 In Progress |
| Phase 4 | Advanced Features | 70% | ✅ Mostly Complete |
| Phase 5 | Production Hardening | 0% | ⬜ Not Started |

### Phase 1: Core Infrastructure ✅ 90%

| Component | Status | Implementation |
|-----------|--------|----------------|
| Go project scaffolding | ✅ Done | `go.mod`, `Makefile`, `Dockerfile` |
| MongoDB models | ✅ Done | `internal/state/models.go` |
| MongoDB store | ✅ Done | `internal/state/store.go` |
| CWL parser (CommandLineTool) | ✅ Done | `internal/cwl/parser.go` |
| CWL parser (Workflow) | ✅ Done | `internal/cwl/workflow.go` |
| CWL types | ✅ Done | `internal/cwl/types.go` |
| REST API routes | ✅ Done | `internal/api/routes.go` |
| REST API handlers | ✅ Done | `internal/api/handlers.go` |
| DAG construction | ✅ Done | `internal/dag/dag.go`, `builder.go` |
| DAG topological sort | ✅ Done | `internal/dag/scheduler.go` |
| Configuration | ✅ Done | `internal/config/config.go` |
| Unit tests | ✅ Done | 92+ tests passing |

### Phase 2: BV-BRC Integration 🟡 40%

| Component | Status | Implementation |
|-----------|--------|----------------|
| Executor interface | ✅ Done | `internal/executor/executor.go` |
| Local executor (dev) | ✅ Done | `internal/executor/local.go` |
| Container runtime abstraction | ✅ Done | `internal/executor/container.go` |
| Docker support | ✅ Done | `buildDockerCommand()` |
| Podman support | ✅ Done | `buildPodmanCommand()` |
| Apptainer support | ✅ Done | `buildApptainerCommand()` |
| Container validation | ✅ Done | `ValidateContainerRequirement()` |
| Redis event publisher | ✅ Done | `internal/events/events.go` |
| Redis event subscription | ⬜ TODO | Needs task_completion handler |
| BV-BRC Task table insert | ⬜ TODO | Needs DB integration |
| CWLStepRunner Perl app | ⬜ TODO | BV-BRC side implementation |
| End-to-end test | ⬜ TODO | Needs infrastructure |

### Phase 3: File Validation & Output 🟡 30%

| Component | Status | Implementation |
|-----------|--------|----------------|
| Staging interface | ✅ Done | `internal/staging/staging.go` |
| Local file stager | ✅ Done | `LocalStager` struct |
| Workspace stager | 🟡 Partial | Interface defined |
| Shock stager | 🟡 Partial | Interface defined |
| File validation | ⬜ TODO | Needs Workspace API |
| Output collection | ⬜ TODO | Needs implementation |

### Phase 4: Advanced Features ✅ 70%

| Component | Status | Implementation |
|-----------|--------|----------------|
| Scatter parsing | ✅ Done | `internal/cwl/scatter.go` |
| Scatter execution | ✅ Done | `internal/dag/scheduler.go` |
| ScatterMethod support | ✅ Done | dotproduct, nested_crossproduct, flat_crossproduct |
| JavaScript expressions | ✅ Done | `internal/cwl/expression.go` (goja) |
| Parameter references | ✅ Done | `$(inputs.x)`, `$(self)`, `$(runtime.x)` |
| Subworkflow parsing | ✅ Done | Workflow class detection |
| Subworkflow execution | 🟡 Partial | Needs recursive DAG |
| Conditional (`when`) | ⬜ TODO | Not implemented |
| GPU/CUDA requirements | ✅ Done | `CUDARequirement` parsing |

### Phase 5: Production Hardening ⬜ 0%

| Component | Status | Notes |
|-----------|--------|-------|
| Error recovery | ⬜ TODO | Config exists (`max_retries`) |
| Workflow resumption | ⬜ TODO | DAG state serialization needed |
| Performance optimization | ⬜ TODO | — |
| Monitoring/alerting | ⬜ TODO | — |
| Documentation | 🟡 Partial | README, CWL guidelines done |

### CWL v1.2 Feature Matrix

| Feature | Status | Notes |
|---------|--------|-------|
| CommandLineTool | ✅ Full | Parsing, command building, validation |
| Workflow | ✅ Full | Step parsing, DAG construction |
| ResourceRequirement | ✅ Full | cores, ram, tmpdir, outdir |
| DockerRequirement | ✅ Full | dockerPull, dockerLoad, dockerFile |
| ApptainerRequirement | ✅ Full | apptainerPull, apptainerFile (extension) |
| CUDARequirement | ✅ Full | GPU count, CUDA version, compute capability |
| ScatterFeatureRequirement | ✅ Full | All scatter methods |
| InlineJavascriptRequirement | ✅ Full | goja engine, expressionLib |
| InitialWorkDirRequirement | ✅ Parsing | Execution not tested |
| EnvVarRequirement | ✅ Full | Environment variable injection |
| SubworkflowFeatureRequirement | 🟡 Partial | Parsing done, execution partial |
| Conditional (`when`) | ⬜ TODO | — |
| ExpressionTool | ⬜ TODO | — |

### Test Coverage

```
internal/cwl      92+ tests  ✅ PASS
internal/dag      15+ tests  ✅ PASS
internal/executor 17+ tests  ✅ PASS
```

### Next Priority Items

1. **Redis task completion subscription** - Handle `task_completion` events
2. **Workspace file validation** - Check file accessibility before workflow start
3. **CWLStepRunner Perl application** - BV-BRC side implementation
4. **End-to-end integration test** - Single-step workflow through local executor
5. **Subworkflow execution** - Recursive DAG building and execution

### Example CWL Tools

The repository includes example CWL tools following authoring guidelines:

- `examples/tools/bwa-mem.cwl` - Sequence alignment with Docker/Apptainer
- `examples/tools/alphafold-predict.cwl` - Structure prediction with GPU/CUDA
- `examples/workflows/align-reads.cwl` - Multi-step alignment pipeline
- `examples/jobs/*.yaml` - Example job submission files

See [CWL_AUTHORING_GUIDELINES.md](./CWL_AUTHORING_GUIDELINES.md) for tool authoring requirements.
