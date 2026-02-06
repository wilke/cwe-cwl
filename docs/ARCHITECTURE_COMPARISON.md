# Architecture vs. README Diagram

This document cross-checks the current implementation against the high-level architecture diagram in the README.

## Implementation Snapshot (2026-02)

- **Entry points**
  - `cmd/cwe-server`: REST API (Chi) for submit/list/status/cancel/rerun/upload/validate. Persists workflow and step state in MongoDB via `internal/state`.
  - `cmd/cwe-scheduler`: Background daemon that polls MongoDB for pending/running workflows, builds DAGs (`internal/dag`) from parsed CWL documents (`internal/cwl`), and advances execution.
  - `cmd/cwe-cli`: User-facing CLI that talks to the REST API and optionally stages files.
- **Execution path**
  - Schedulers dispatch steps through `internal/executor`:
    - `NewAppServiceExecutor` submits to BV-BRC `app_service` (default) and hands off to the registered `CWLStepRunner` application.
    - `NewDBExecutor` (when `executor.mode=bvbrc`) uses the BV-BRC PostgreSQL DB; `NewLocalExecutor` exists for dev.
  - Step payload is executed by the `cmd/cwl-step-runner` binary inside SLURM jobs; it materializes commands, runs them, and writes `cwl_outputs.json`.
  - JavaScript expressions are evaluated by the `cmd/sandbox-worker` helper (isolated goja sandbox).
- **Events and monitoring**
  - Redis (if configured) is used for task completion pub/sub via `internal/events` (`task_completion` channel). The scheduler falls back to polling `app_service` when Redis events are unavailable.
- **Data and storage**
  - MongoDB stores workflow definitions and run/step state.
  - File staging/backends handled in `internal/staging` with Workspace/Shock/local options from `configs/config*.yaml`.
  - BV-BRC integration points (App Service URL, DB DSN, CWLStepRunner ID) are defined in `internal/config`.

## Mapping to the Diagram

| Diagram node | Implementation evidence | Notes |
| --- | --- | --- |
| REST API (Chi) | `cmd/cwe-server`, `internal/api` | Matches diagram. |
| CWL Parser (v1.2) | `internal/cwl` parser/types/expression handling | Matches. |
| DAG Scheduler | `cmd/cwe-scheduler`, `internal/dag` | Scheduler is a separate daemon, not part of the API process. |
| BV-BRC Executor | `internal/executor` (`app_service`/`bvbrc`/`local` modes) + `cmd/cwl-step-runner` | Diagram shows a single executor box; implementation includes multiple executor backends and an explicit step-runner binary. |
| State Store (MongoDB) | `internal/state`, `configs/config*.yaml` | Matches. |
| Event Processor (Redis PubSub) | `internal/events` subscriber/publisher | Matches; polling fallback exists when Redis is absent. |
| External systems (MongoDB, Redis, app_service → SLURM) | Configured in `configs/` and consumed in `cmd/cwe-scheduler`/`internal/executor` | Matches diagram flow to SLURM via app_service. |

## Elements Not Shown in the Diagram

- **CLI client (`cmd/cwe-cli`)**: Primary user entry point for uploads, submission, and status queries.
- **Sandbox/expression runner (`cmd/sandbox-worker`)**: Executes CWL JavaScript expressions safely.
- **File staging layer (`internal/staging`)**: Manages Workspace/Shock/local paths; not represented in the diagram.
- **BV-BRC DB dependency**: `executor.mode=bvbrc` connects to PostgreSQL; diagram only shows app_service.
- **Local executor mode**: Development pathway that bypasses BV-BRC; absent from the diagram.
- **Scheduler-event separation**: Event subscriber/publisher live with the scheduler daemon, not the API server.
