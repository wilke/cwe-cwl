# CWE-CWL: CWL Workflow Engine for BV-BRC

A Go-based CWL (Common Workflow Language) v1.2 workflow execution system that integrates with BV-BRC's existing SLURM infrastructure.

## Overview

CWE-CWL provides a workflow execution engine that:

- Parses and validates CWL v1.2 documents
- Builds DAGs from workflow step dependencies
- Submits each step as a BV-BRC Task via app_service (CWLStepRunner) for SLURM execution
- Monitors execution via BV-BRC task status (app_service API; optional Redis events)
- Stores workflow state in MongoDB for persistence and recovery

## Components

| Component | Description |
|-----------|-------------|
| `cwe-server` | REST API server for workflow submission and status queries |
| `cwe-scheduler` | Background daemon that schedules workflow steps |
| `cwe-cli` | Command-line interface for users |

## Quick Start

### Prerequisites

- Go 1.21+
- MongoDB 6+
- Redis 6+
- BV-BRC infrastructure (for production mode)

### Build

```bash
make deps
make build
```

### Run (Development Mode)

```bash
# Start MongoDB and Redis (e.g., via Docker)
docker run -d --name mongo -p 27017:27017 mongo:6
docker run -d --name redis -p 6379:6379 redis:6

# Run server
./bin/cwe-server -config configs/config.dev.yaml

# Run scheduler (in another terminal)
./bin/cwe-scheduler -config configs/config.dev.yaml
```

### CLI Usage

```bash
# Set authentication token
export BVBRC_TOKEN="your-p3-token"

# Validate a workflow
./bin/cwe-cli validate workflow.cwl

# Submit a workflow
./bin/cwe-cli submit workflow.cwl job.yaml --output /user@patricbrc.org/home/results/

# Check status
./bin/cwe-cli status <workflow-id>

# Watch status until completion
./bin/cwe-cli status <workflow-id> --watch

# List workflows
./bin/cwe-cli list

# Get outputs
./bin/cwe-cli outputs <workflow-id>

# Get step details
./bin/cwe-cli steps <workflow-id>

# Cancel a workflow
./bin/cwe-cli cancel <workflow-id>
```

## REST API

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/workflows` | Submit workflow |
| GET | `/api/v1/workflows` | List user's workflows |
| GET | `/api/v1/workflows/{id}` | Get workflow status |
| DELETE | `/api/v1/workflows/{id}` | Cancel workflow |
| POST | `/api/v1/workflows/{id}/rerun` | Rerun failed workflow |
| GET | `/api/v1/workflows/{id}/steps` | Get step statuses |
| GET | `/api/v1/workflows/{id}/outputs` | Get workflow outputs |
| POST | `/api/v1/validate` | Validate CWL document |
| POST | `/api/v1/validate-inputs` | Validate input files |
| POST | `/api/v1/upload` | Upload file to local storage |
| GET | `/api/v1/files/{id}` | Download cached file |

## Admin REST API

Admin endpoints require a user in `auth.admin_users`.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/admin/workflows` | List workflows across users |
| GET | `/api/v1/admin/workflows/{id}` | Get workflow status across users |
| DELETE | `/api/v1/admin/workflows/{id}` | Cancel workflow across users |
| POST | `/api/v1/admin/workflows/{id}/rerun` | Rerun workflow across users |
| GET | `/api/v1/admin/workflows/{id}/steps` | List steps across users |
| POST | `/api/v1/admin/workflows/{id}/steps/{step_id}/requeue` | Requeue a step (optional `scatter_index=0,1`) |

## Configuration

Configuration is loaded from YAML files. Environment variables can override settings with the `CWE_` prefix (e.g., `CWE_AUTH_SERVICE_TOKEN`).

See `configs/config.yaml` for all available options.

## CWL v1.2 Feature Support

| Feature | Status |
|---------|--------|
| CommandLineTool | Supported |
| Workflow | Supported |
| ResourceRequirement | Supported |
| DockerRequirement | Supported |
| Scatter/ScatterMethod | Supported |
| SubworkflowFeatureRequirement | Planned |
| InlineJavascriptRequirement | Supported (via goja) |
| Conditional (when) | Planned |
| ExpressionTool | Planned |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                CWL Workflow Service (Go)                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  REST API   │  │ CWL Parser  │  │  DAG Scheduler      │  │
│  │  (Chi)      │  │  (v1.2)     │  │  (Dependency Mgmt)  │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ BV-BRC      │  │ State Store │  │  Event Processor    │  │
│  │ Executor    │  │ (MongoDB)   │  │  (Redis PubSub)     │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└───────────────────────────┬─────────────────────────────────┘
                            │
            ┌───────────────┼───────────────┐
            ▼               ▼               ▼
     ┌──────────┐    ┌──────────┐    ┌─────────────┐
     │ MongoDB  │    │  Redis   │    │ BV-BRC      │
     │(CWL State)│   │(Events)  │    │ app_service │
     └──────────┘    └──────────┘    └──────┬──────┘
                                            │
                                            ▼
                                     ┌─────────────┐
                                     │   SLURM     │
                                     │  Cluster    │
                                     └─────────────┘
```

For a current mapping between the diagram and the implementation (including components not depicted like the CLI, sandbox worker, and executor backends), see [`docs/ARCHITECTURE_COMPARISON.md`](docs/ARCHITECTURE_COMPARISON.md).

## Development

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Format code
make fmt

# Run linter
make lint

# Install dev tools
make tools
```

## License

See LICENSE file.
