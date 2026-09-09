package runner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/filfreire/idlebat/internal/config"
	"github.com/filfreire/idlebat/internal/provider"
	"github.com/filfreire/idlebat/internal/state"
)

var terminalMu sync.Mutex

// RunInternal runs one persisted job. It is also used by spawned terminal processes.
func RunInternal(stateDir, stepID string) int {
	logFile := filepath.Join(stateDir, "logs", stepID+".log")
	streamLog := filepath.Join(stateDir, "logs", stepID+".stream.jsonl")
	stderrLog := filepath.Join(stateDir, "logs", stepID+".stderr.log")
	exitFile := filepath.Join(stateDir, "status", stepID+".exit")
	doneFile := filepath.Join(stateDir, "status", stepID+".done")
	promptFile := filepath.Join(stateDir, "prompts", stepID+".txt")

	cfg, err := state.ReadRunConfig(stateDir)
	if err != nil {
		return fail(logFile, exitFile, doneFile, "Failed to read config: %s\n", err)
	}
	job, err := state.ReadJob(stateDir, stepID)
	if err != nil {
		// Compatibility for callers created before per-job state existed.
		job = &config.ExpandedStep{
			ID:       stepID,
			Name:     stepID,
			Agent:    "claude",
			Provider: "claude",
			Model:    cfg.Model,
			WorkDir:  cfg.WorkDir,
		}
	}

	workDir := job.WorkDir
	if workDir == "" {
		workDir = cfg.WorkDir
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return fail(logFile, exitFile, doneFile, "Failed to resolve work dir %s: %s\n", workDir, err)
	}
	if err := os.MkdirAll(absWorkDir, 0755); err != nil {
		return fail(logFile, exitFile, doneFile, "Failed to create work dir %s: %s\n", absWorkDir, err)
	}

	promptData, err := os.ReadFile(promptFile)
	if err != nil {
		return fail(logFile, exitFile, doneFile, "Failed to read prompt file: %s\n", err)
	}

	banner := fmt.Sprintf("\n========================================================\n  %s -- Job: %s (%s)\n  Work dir: %s\n========================================================\n", cfg.Name, stepID, job.Provider, absWorkDir)
	printLocked(banner)
	appendToFile(logFile, banner)

	artifactsDir := filepath.Join(stateDir, "artifacts")
	cmd, err := provider.BuildCommand(*job, absWorkDir, artifactsDir, string(promptData))
	if err != nil {
		return fail(logFile, exitFile, doneFile, "Failed to build provider command: %s\n", err)
	}
	cmd.Env = os.Environ()
	if cfg.EnvFile != "" {
		env, err := readEnvFile(cfg.EnvFile)
		if err != nil {
			appendToFile(logFile, fmt.Sprintf("Warning: failed to load env file %s: %s\n", cfg.EnvFile, err))
		} else {
			for key, value := range env {
				cmd.Env = setEnv(cmd.Env, key, value)
			}
		}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(logFile, exitFile, doneFile, "Failed to create stdout pipe: %s\n", err)
	}
	stderrFile, err := os.Create(stderrLog)
	if err != nil {
		return fail(logFile, exitFile, doneFile, "Failed to create stderr log: %s\n", err)
	}
	defer stderrFile.Close()
	cmd.Stderr = io.MultiWriter(stderrFile, lockedWriter{writer: os.Stderr})

	streamFile, err := os.Create(streamLog)
	if err != nil {
		return fail(logFile, exitFile, doneFile, "Failed to create stream log: %s\n", err)
	}
	defer streamFile.Close()

	if err := cmd.Start(); err != nil {
		return fail(logFile, exitFile, doneFile, "Failed to start %s: %s\n", job.Provider, err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	finalText := ""
	events := 0
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(streamFile, line)
		events++
		if candidate := provider.FinalText(job.Provider, line); candidate != "" {
			finalText = candidate
		}
		if events == 1 || events%25 == 0 {
			printLocked(fmt.Sprintf("[%s] %d events received\n", stepID, events))
		}
	}

	exitCode := 0
	if err := scanner.Err(); err != nil {
		appendToFile(logFile, fmt.Sprintf("stream read failed: %s\n", err))
		exitCode = 1
	}
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	if finalText != "" {
		appendToFile(logFile, "\n--- Final agent response ---\n"+finalText+"\n")
	}
	if exitCode == 0 && job.Output != "" {
		if finalText == "" {
			appendToFile(logFile, "Provider completed without a final response to save.\n")
			exitCode = 1
		} else {
			outputPath := filepath.Join(artifactsDir, filepath.Clean(job.Output))
			if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
				appendToFile(logFile, fmt.Sprintf("creating output directory: %s\n", err))
				exitCode = 1
			} else if err := os.WriteFile(outputPath, []byte(finalText+"\n"), 0644); err != nil {
				appendToFile(logFile, fmt.Sprintf("writing output artifact: %s\n", err))
				exitCode = 1
			}
		}
	}
	if exitCode == 0 {
		for _, checkFile := range job.CheckFiles {
			checkPath := checkFile
			if !filepath.IsAbs(checkPath) {
				checkPath = filepath.Join(absWorkDir, checkPath)
			}
			if _, err := os.Stat(checkPath); err != nil {
				appendToFile(logFile, fmt.Sprintf("Expected file missing: %s\n", checkFile))
				exitCode = 1
			}
		}
	}

	completion := fmt.Sprintf("[%s] complete (provider=%s, exit=%d, events=%d)\n", stepID, job.Provider, exitCode, events)
	printLocked(completion)
	appendToFile(logFile, completion)
	writeExitAndDone(exitFile, doneFile, exitCode)
	return exitCode
}

func fail(logFile, exitFile, doneFile, format string, args ...interface{}) int {
	msg := fmt.Sprintf(format, args...)
	printLocked(msg)
	appendToFile(logFile, msg)
	writeExitAndDone(exitFile, doneFile, 1)
	return 1
}

func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		result[key] = value
	}
	return result, scanner.Err()
}

// loadEnvFile is retained for compatibility with older callers and tests.
func loadEnvFile(path string) error {
	env, err := readEnvFile(path)
	if err != nil {
		return err
	}
	for key, value := range env {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

// extractTextFromStreamLog retains the original Claude log extraction helper.
func extractTextFromStreamLog(streamLogPath, logFilePath string) {
	data, err := os.ReadFile(streamLogPath)
	if err != nil {
		return
	}
	var extracted []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		if text := provider.FinalText("claude", line); text != "" {
			extracted = append(extracted, text)
		}
	}
	if len(extracted) > 0 {
		appendToFile(logFilePath, strings.Join(extracted, "\n")+"\n")
	}
}

func writeExitAndDone(exitFile, doneFile string, code int) {
	_ = os.WriteFile(exitFile, []byte(fmt.Sprintf("%d", code)), 0644)
	_ = os.WriteFile(doneFile, []byte{}, 0644)
}

func appendToFile(path, text string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = io.WriteString(f, text)
}

func printLocked(text string) {
	terminalMu.Lock()
	defer terminalMu.Unlock()
	fmt.Print(text)
}

type lockedWriter struct {
	writer io.Writer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	terminalMu.Lock()
	defer terminalMu.Unlock()
	return w.writer.Write(p)
}
