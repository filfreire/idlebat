package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/filfreire/idlebat/internal/config"
	"github.com/filfreire/idlebat/internal/display"
	"github.com/filfreire/idlebat/internal/runner"
	"github.com/filfreire/idlebat/internal/state"
)

var gitWorktreeMu sync.Mutex

type Result struct {
	ID       string
	Name     string
	Provider string
	ExitCode int
	Duration time.Duration
	Skipped  bool
	Blocked  bool
}

type jobCompletion struct {
	result Result
}

// Run executes all jobs whose dependencies are satisfied, up to MaxParallel at once.
func Run(cfg *config.Config, jobs []config.ExpandedStep, stateDir string, resume bool) ([]Result, error) {
	status := make(map[string]string, len(jobs))
	results := make(map[string]Result, len(jobs))
	for _, job := range jobs {
		status[job.ID] = "pending"
	}

	runtimeValues := map[string]string{
		"artifacts_dir": filepath.Join(stateDir, "artifacts"),
		"state_dir":     stateDir,
		"work_dir":      cfg.WorkDir,
	}
	for _, job := range jobs {
		if job.Output == "" {
			continue
		}
		outputPath := filepath.Join(stateDir, "artifacts", filepath.Clean(job.Output))
		runtimeValues["output."+job.ID] = outputPath
		runtimeValues["outputs."+job.ID] = outputPath
	}

	if resume {
		for _, job := range jobs {
			if completedSuccessfully(stateDir, job) {
				status[job.ID] = "success"
				results[job.ID] = Result{ID: job.ID, Name: job.Name, Provider: job.Provider, Skipped: true}
				display.LogSuccess(fmt.Sprintf("Resume: keeping completed job %s", job.ID))
			}
		}
	}

	completionCh := make(chan jobCompletion, len(jobs))
	running := 0
	finished := countFinished(status)

	for finished < len(jobs) {
		madeProgress := false

		for _, job := range jobs {
			if status[job.ID] != "pending" {
				continue
			}
			if dependencyFailed(job, status) {
				status[job.ID] = "blocked"
				results[job.ID] = Result{ID: job.ID, Name: job.Name, Provider: job.Provider, ExitCode: 1, Blocked: true}
				finished++
				madeProgress = true
				display.LogErr(fmt.Sprintf("Job %s blocked by a failed dependency", job.ID))
			}
		}

		for _, original := range jobs {
			if running >= cfg.MaxParallel {
				break
			}
			if status[original.ID] != "pending" || !dependenciesPassed(original, status) {
				continue
			}
			job := original
			job.Prompt = config.RenderRuntimeVariables(job.Prompt, runtimeValues)
			job.Output = config.RenderRuntimeVariables(job.Output, runtimeValues)
			if strings.Contains(job.Prompt, "{{") {
				return orderedResults(jobs, results), fmt.Errorf("job %q prompt contains an unresolved template variable", job.ID)
			}
			status[job.ID] = "running"
			running++
			madeProgress = true
			display.LogInfo(fmt.Sprintf("Starting %s with %s", job.ID, job.Provider))
			go func() {
				completionCh <- jobCompletion{result: runJob(cfg, job, stateDir)}
			}()
		}

		if running == 0 {
			if !madeProgress && finished < len(jobs) {
				return orderedResults(jobs, results), fmt.Errorf("workflow made no progress; dependency graph is stuck")
			}
			continue
		}

		completion := <-completionCh
		running--
		finished++
		result := completion.result
		results[result.ID] = result
		if result.ExitCode == 0 {
			status[result.ID] = "success"
			display.LogSuccess(fmt.Sprintf("Job %s completed (%s)", result.ID, display.FormatDuration(result.Duration)))
		} else {
			status[result.ID] = "failed"
			display.LogErr(fmt.Sprintf("Job %s failed with exit %d (%s)", result.ID, result.ExitCode, display.FormatDuration(result.Duration)))
		}
	}

	return orderedResults(jobs, results), nil
}

func runJob(cfg *config.Config, job config.ExpandedStep, stateDir string) Result {
	start := time.Now()
	result := Result{ID: job.ID, Name: job.Name, Provider: job.Provider, ExitCode: 1}
	workDir := cfg.WorkDir
	cleanup := func() {}
	if job.Isolation == "git-worktree" {
		isolatedDir := filepath.Join(stateDir, "worktrees", job.ID)
		if err := addWorktree(cfg.WorkDir, isolatedDir); err != nil {
			writeFailureStatus(stateDir, job.ID, err)
			result.Duration = time.Since(start)
			return result
		}
		workDir = isolatedDir
		cleanup = func() {
			if err := removeWorktree(cfg.WorkDir, isolatedDir); err != nil {
				display.LogErr(fmt.Sprintf("Failed to remove worktree for %s: %s", job.ID, err))
			}
		}
	}
	defer cleanup()

	job.WorkDir = workDir
	if err := state.WriteJob(stateDir, job); err != nil {
		writeFailureStatus(stateDir, job.ID, err)
		result.Duration = time.Since(start)
		return result
	}
	promptPath := filepath.Join(stateDir, "prompts", job.ID+".txt")
	if err := os.WriteFile(promptPath, []byte(job.Prompt), 0644); err != nil {
		writeFailureStatus(stateDir, job.ID, err)
		result.Duration = time.Since(start)
		return result
	}
	_ = os.Remove(filepath.Join(stateDir, "status", job.ID+".done"))
	_ = os.Remove(filepath.Join(stateDir, "status", job.ID+".exit"))

	result.ExitCode = runner.RunInternal(stateDir, job.ID)
	result.Duration = time.Since(start)
	return result
}

func addWorktree(repoDir, target string) error {
	gitWorktreeMu.Lock()
	defer gitWorktreeMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	cmd := exec.Command("git", "-C", repoDir, "worktree", "add", "--detach", target, "HEAD")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating isolated worktree: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func removeWorktree(repoDir, target string) error {
	gitWorktreeMu.Lock()
	defer gitWorktreeMu.Unlock()
	cmd := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", target)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("removing isolated worktree: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeFailureStatus(stateDir, jobID string, err error) {
	_ = os.WriteFile(filepath.Join(stateDir, "logs", jobID+".log"), []byte(err.Error()+"\n"), 0644)
	_ = os.WriteFile(filepath.Join(stateDir, "status", jobID+".exit"), []byte("1"), 0644)
	_ = os.WriteFile(filepath.Join(stateDir, "status", jobID+".done"), []byte{}, 0644)
}

func completedSuccessfully(stateDir string, job config.ExpandedStep) bool {
	if _, err := os.Stat(filepath.Join(stateDir, "status", job.ID+".done")); err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "status", job.ID+".exit"))
	if err != nil {
		return false
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || code != 0 {
		return false
	}
	if job.Output != "" {
		if _, err := os.Stat(filepath.Join(stateDir, "artifacts", filepath.Clean(job.Output))); err != nil {
			return false
		}
	}
	return true
}

func dependenciesPassed(job config.ExpandedStep, status map[string]string) bool {
	for _, need := range job.Needs {
		if status[need] != "success" {
			return false
		}
	}
	return true
}

func dependencyFailed(job config.ExpandedStep, status map[string]string) bool {
	for _, need := range job.Needs {
		if status[need] == "failed" || status[need] == "blocked" {
			return true
		}
	}
	return false
}

func countFinished(status map[string]string) int {
	count := 0
	for _, value := range status {
		if value == "success" || value == "failed" || value == "blocked" {
			count++
		}
	}
	return count
}

func orderedResults(jobs []config.ExpandedStep, byID map[string]Result) []Result {
	results := make([]Result, 0, len(jobs))
	for _, job := range jobs {
		if result, ok := byID[job.ID]; ok {
			results = append(results, result)
		}
	}
	return results
}
