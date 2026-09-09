# idlebat

Run repeatable local agent workflows from YAML. idlebat supports Claude Code,
Codex CLI, and Cursor Agent, including parallel jobs and dependency-based
fan-in.

Works on macOS, Linux, and Windows.

## Install

macOS / Linux:

```sh
curl -fsSL https://filfreire.com/idlebat/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://filfreire.com/idlebat/install.ps1 | iex
```

## Requirements

Install and authenticate each CLI used by your workflow:

- `claude` for Claude Code jobs
- `codex` for Codex jobs
- `agent` for Cursor Agent jobs

## Quick start

```sh
# Run a workflow
idlebat my-workflow.yaml

# Preview the resolved jobs
idlebat my-workflow.yaml --dry-run

# Supply reusable template variables
idlebat my-workflow.yaml --var pr=5782 --var action=draft

# Override the workflow directory and concurrency
idlebat my-workflow.yaml --work-dir "$PWD" --max-parallel 3

# Resume the latest run if its config and git HEAD still match
idlebat my-workflow.yaml --var pr=5782 --resume
```

## YAML v2

```yaml
version: 2
name: "Review PR {{pr}}"
work_dir: .
max_parallel: 3

variables:
  action: draft

agents:
  claude-reviewer:
    provider: claude
    model: "claude-opus-5[1m]"

  codex-reviewer:
    provider: codex

  cursor-reviewer:
    provider: cursor

jobs:
  - id: review-a
    name: "Claude review"
    agent: claude-reviewer
    isolation: git-worktree
    prompt_file: ./prompts/review.txt
    variables:
      letter: A
    output: findings/A-claude.md

  - id: review-b
    name: "Codex review"
    agent: codex-reviewer
    isolation: git-worktree
    prompt_file: ./prompts/review.txt
    variables:
      letter: B
    output: findings/B-codex.md

  - id: consolidate
    name: "Validate and consolidate"
    agent: codex-reviewer
    needs: [review-a, review-b]
    prompt: |
      Validate and consolidate these independent reviews:
      - {{outputs.review-a}}
      - {{outputs.review-b}}
    output: consolidated/review.md
```

Jobs with no unmet dependencies start concurrently, up to `max_parallel`.
A job starts only after every job in `needs` succeeds. If a dependency fails,
dependent jobs are reported as blocked.

### Providers

An agent has a `provider`, an optional `model`, and optional extra `args`:

```yaml
agents:
  thorough-codex:
    provider: codex
    model: gpt-5.3-codex
    args: ["--search"]
```

The built-in non-interactive invocations are:

- Claude: `claude -p --dangerously-skip-permissions --output-format stream-json`
- Codex: `codex exec --json --ephemeral --dangerously-bypass-approvals-and-sandbox`
- Cursor: `agent -p --force --sandbox disabled --approve-mcps --trust --output-format stream-json`

These use the same permission-bypass flags as the corresponding `claude-root`,
`codex-root`, and `agent-root` aliases. They are intended for trusted code and
have unrestricted access to the host available to each provider process.

idlebat captures the provider's final response as the job's `output`; the
agent does not need to create the artifact itself. Raw JSONL and stderr are
preserved under the run's `logs` directory.

### Job fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Unique job identifier |
| `name` | no | Human-readable name; defaults to `id` |
| `agent` | no | Named agent or provider; defaults to `claude` |
| `prompt` | * | Inline prompt |
| `prompt_file` | * | Prompt file relative to the YAML file |
| `needs` | no | Job IDs that must succeed first |
| `output` | no | Final-response artifact path, relative to the run |
| `isolation` | no | `shared` or disposable `git-worktree` |
| `variables` | no | Job-specific template values |
| `when` | no | Variable/value pairs that must all match for the job to be included |
| `repeat` | no | Expand the job multiple times |
| `check_files` | no | Files that must exist after the job |

One of `prompt` or `prompt_file` is required.

### Template variables

Top-level and job variables use `{{name}}`. Command-line `--var` values take
precedence over top-level defaults. These values are also available:

| Variable | Description |
|----------|-------------|
| `{{iteration}}` | Current repeat iteration, starting at 1 |
| `{{repeat}}` | Total repeat count |
| `{{job_id}}` | Expanded job ID |
| `{{job_name}}` | Expanded job name |
| `{{outputs.JOB_ID}}` | Absolute artifact path for another job |
| `{{artifacts_dir}}` | Run artifact directory |
| `{{state_dir}}` | Full run state directory |
| `{{work_dir}}` | Main workflow working directory |

Unresolved template variables are rejected before agent execution.

### Isolation

`isolation: git-worktree` creates a detached worktree at the current `HEAD`,
runs the job there, and removes it when the job finishes. Use this for parallel
reviewers so incidental file changes do not collide. Use `shared` for a job
that intentionally needs to edit the original worktree.

Provider CLIs are granted access to the run artifact directory so downstream
jobs can read captured outputs.

## PR review workflow

The bundled [`examples/pr-review.yaml`](examples/pr-review.yaml) runs three
independent reviews, then asks Codex to validate and consolidate them.

From an existing PR worktree:

```sh
idlebat examples/pr-review.yaml \
  --work-dir "$PWD" \
  --var pr=5782
```

The default `action=draft` only produces a proposed review. To apply validated
fixes in the original worktree, commit them, and push the current branch:

```sh
idlebat examples/pr-review.yaml \
  --work-dir "$PWD" \
  --var pr=5782 \
  --var action=fix
```

Draft mode does not modify files or GitHub state. Fix mode uses a separate
publishing job after consolidation to create one normal commit and push it to
the current branch's configured upstream. It never force-pushes, posts a
GitHub review, approves the PR, or merges it. Publishing details are captured
in `consolidated/publish.md` under the run's artifact directory.

A convenient zsh wrapper that preserves the existing `pnew` flow is:

```zsh
pr-review() {
  local pr="$1"
  local action="${2:-draft}"
  pnew pr "$pr" || return
  idlebat /Users/filipe/work/idlebat/examples/pr-review.yaml \
    --work-dir "$PWD" \
    --var "pr=$pr" \
    --var "action=$action"
}
```

## Run state and resume

Run data is stored under:

```text
${IDLEBAT_STATE_DIR:-${XDG_STATE_HOME:-~/.local/state}/idlebat}/<workflow>/<run-id>/
```

Each run contains:

- `manifest.json` with the effective config hash and git HEAD
- `artifacts/` with captured final responses
- `logs/` with raw provider output and stderr
- `prompts/` with the exact prompts sent
- `status/` with per-job completion status

`--resume` refuses to reuse state after the effective variables, work
directory, concurrency, YAML, or git HEAD changes.

## Legacy YAML

Existing `steps` configurations remain supported. Legacy steps retain serial
execution and default to Claude:

```yaml
name: "My Task"
work_dir: ~/projects/my-app
model: claude-sonnet-4-6

steps:
  - id: implement
    prompt: "Implement the feature"
  - id: review
    prompt: "Review and fix the implementation"
```

## Uninstall

macOS / Linux:

```sh
rm /usr/local/bin/idlebat
# or: rm ~/.local/bin/idlebat
```

Windows (PowerShell):

```powershell
Remove-Item "$env:LOCALAPPDATA\idlebat" -Recurse -Force
```

## Development

```sh
go test ./...
go build -o idlebat ./cmd/idlebat
```
