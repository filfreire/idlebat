# idlebat

Orchestrate sequential Claude agents from a YAML config file. Each step runs in its own terminal window/tab.

Works on macOS, Linux, and Windows.

## Install

**macOS / Linux:**

```sh
curl -fsSL https://filfreire.com/idlebat/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://filfreire.com/idlebat/install.ps1 | iex
```

## Uninstall

**macOS / Linux:**

```sh
rm /usr/local/bin/idlebat
# or: rm ~/.local/bin/idlebat
```

**Windows (PowerShell):**

```powershell
Remove-Item "$env:LOCALAPPDATA\idlebat" -Recurse -Force
# then remove the idlebat entry from your user PATH in System > Environment Variables
```

## Requirements

- [Claude CLI](https://docs.anthropic.com/en/docs/claude-cli) installed and authenticated

## Quick Start

```sh
# Run a workflow
idlebat my-workflow.yaml

# Preview steps without executing
idlebat my-workflow.yaml --dry-run

# Resume after interruption (skips completed steps)
idlebat my-workflow.yaml --resume
```

## YAML Config Format

```yaml
name: "My Task"
work_dir: ~/projects/my-app        # working directory for agents
model: claude-opus-4-6             # optional, default: claude-sonnet-4-6
env_file: ~/.env.secrets           # optional: KEY=VALUE file loaded before each step

steps:
  - id: implement-feature
    name: "Implement the feature"
    prompt: |
      Inline prompt text...
    check_files:                   # optional: verify these exist after step
      - src/feature.py

  - id: review
    name: "Review & fix"
    prompt_file: ./prompts/review.txt   # load prompt from file
    repeat: 3                           # run this step 3 times
```

### Step Fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Unique identifier for the step |
| `name` | yes | Human-readable step name |
| `prompt` | * | Inline prompt text |
| `prompt_file` | * | Path to file containing the prompt (relative to config file) |
| `repeat` | no | Number of times to run this step (default: 1) |
| `check_files` | no | List of files to verify exist after the step completes |

\* One of `prompt` or `prompt_file` is required.

### Template Variables

Prompts support these variables, useful with `repeat`:

| Variable | Description |
|----------|-------------|
| `{{iteration}}` | Current iteration number (1-based) |
| `{{repeat}}` | Total number of repetitions |
| `{{step_id}}` | The step's `id` field |
| `{{step_name}}` | The step's `name` field |

## How It Works

1. Parses the YAML config into step definitions
2. For each step, writes the prompt to a temp file
3. Spawns a new terminal window/tab running `idlebat --internal-run` for the step
4. The spawned process runs `claude` with `--output-format stream-json`
5. The orchestrator monitors `stream-json` output for real-time progress
6. Waits for completion, records exit code and duration
7. Optionally checks that expected files were created
8. Prints a summary report at the end

### Platform-Specific Terminal Spawning

| Platform | Method |
|----------|--------|
| macOS | Opens a new Terminal.app tab via `osascript` |
| Windows | Uses Windows Terminal (`wt new-tab`) if available, falls back to `cmd /c start` |
| Linux | Tries `gnome-terminal`, `konsole`, `xfce4-terminal`, `xterm` in order |

State is stored in `{temp}/idlebat-<name>/` with subdirectories for prompts, logs, and status files.

## Examples

See [`examples/`](examples/) for sample configs:

- [`todo-app.yaml`](examples/todo-app.yaml) — simple 3-step workflow with repeat
- [`multi-step.yaml`](examples/multi-step.yaml) — uses `prompt_file`, `model`, and `check_files`

## Development

```sh
go build -o idlebat ./cmd/idlebat
go test ./...
```
