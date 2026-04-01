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

	"github.com/filfreire/idlebat/internal/state"
)

// RunInternal is the entry point for --internal-run mode.
// It runs inside the spawned terminal window.
func RunInternal(stateDir, stepID string) int {
	logFile := filepath.Join(stateDir, "logs", stepID+".log")
	streamLog := filepath.Join(stateDir, "logs", stepID+".stream.jsonl")
	exitFile := filepath.Join(stateDir, "status", stepID+".exit")
	doneFile := filepath.Join(stateDir, "status", stepID+".done")
	promptFile := filepath.Join(stateDir, "prompts", stepID+".txt")

	cfg, err := state.ReadRunConfig(stateDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read config: %s\n", err)
		writeExitAndDone(exitFile, doneFile, 1)
		return 1
	}

	// Banner
	banner := fmt.Sprintf(`
========================================================
  %s -- Step: %s
  Prompt: %s
========================================================
`, cfg.Name, stepID, promptFile)
	fmt.Print(banner)
	appendToFile(logFile, banner)

	// Change to work directory
	if err := os.Chdir(cfg.WorkDir); err != nil {
		msg := fmt.Sprintf("Failed to change to work dir %s: %s\n", cfg.WorkDir, err)
		fmt.Print(msg)
		appendToFile(logFile, msg)
		writeExitAndDone(exitFile, doneFile, 1)
		return 1
	}

	// Load env file if specified
	if cfg.EnvFile != "" {
		if err := loadEnvFile(cfg.EnvFile); err != nil {
			msg := fmt.Sprintf("Warning: failed to load env file %s: %s\n", cfg.EnvFile, err)
			fmt.Print(msg)
			appendToFile(logFile, msg)
		} else {
			msg := fmt.Sprintf("Loaded env file: %s\n", cfg.EnvFile)
			fmt.Print(msg)
			appendToFile(logFile, msg)
		}
	}

	// Read prompt
	promptData, err := os.ReadFile(promptFile)
	if err != nil {
		msg := fmt.Sprintf("Failed to read prompt file: %s\n", err)
		fmt.Print(msg)
		appendToFile(logFile, msg)
		writeExitAndDone(exitFile, doneFile, 1)
		return 1
	}

	fmt.Println("--- Launching Claude agent (stream-json mode) ---")
	appendToFile(logFile, "--- Launching Claude agent (stream-json mode) ---\n")

	// Build claude command
	cmd := exec.Command("claude",
		"-p",
		"--verbose",
		"--model", cfg.Model,
		"--permission-mode", "bypassPermissions",
		"--output-format", "stream-json",
	)
	cmd.Stdin = strings.NewReader(string(promptData))
	cmd.Dir = cfg.WorkDir

	// Capture stdout (stream-json) and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		msg := fmt.Sprintf("Failed to create stdout pipe: %s\n", err)
		fmt.Print(msg)
		appendToFile(logFile, msg)
		writeExitAndDone(exitFile, doneFile, 1)
		return 1
	}
	cmd.Stderr = os.Stderr

	// Open stream log file
	streamFile, err := os.Create(streamLog)
	if err != nil {
		msg := fmt.Sprintf("Failed to create stream log: %s\n", err)
		fmt.Print(msg)
		appendToFile(logFile, msg)
		writeExitAndDone(exitFile, doneFile, 1)
		return 1
	}
	defer streamFile.Close()

	if err := cmd.Start(); err != nil {
		msg := fmt.Sprintf("Failed to start claude: %s\n", err)
		fmt.Print(msg)
		appendToFile(logFile, msg)
		writeExitAndDone(exitFile, doneFile, 1)
		return 1
	}

	// Read stream-json output, tee to file and terminal
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(streamFile, line)
		fmt.Println(line)
	}

	claudeExit := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			claudeExit = exitErr.ExitCode()
		} else {
			claudeExit = 1
		}
	}

	// Extract readable text from stream-json into log
	appendToFile(logFile, "\n--- Claude Output (extracted from stream-json) ---\n")
	extractTextFromStreamLog(streamLog, logFile)

	completionMsg := fmt.Sprintf(`
========================================================
  Step %s COMPLETE (exit: %d)
========================================================
`, stepID, claudeExit)
	fmt.Print(completionMsg)
	appendToFile(logFile, completionMsg)

	writeExitAndDone(exitFile, doneFile, claudeExit)
	return claudeExit
}

// loadEnvFile reads a KEY=VALUE file and sets environment variables.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Strip surrounding quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		os.Setenv(key, val)
	}
	return scanner.Err()
}

// extractTextFromStreamLog reads stream-json and appends readable text to logFile.
func extractTextFromStreamLog(streamLogPath, logFilePath string) {
	data, err := os.ReadFile(streamLogPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	var extracted []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		msgType, _ := obj["type"].(string)
		if msgType != "assistant" && msgType != "result" {
			continue
		}
		msg, ok := obj["message"].(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, block := range content {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if b["type"] == "text" {
				if text, ok := b["text"].(string); ok && text != "" {
					extracted = append(extracted, text)
				}
			}
		}
	}
	if len(extracted) > 0 {
		appendToFile(logFilePath, strings.Join(extracted, "\n")+"\n")
	}
}

func writeExitAndDone(exitFile, doneFile string, code int) {
	os.WriteFile(exitFile, []byte(fmt.Sprintf("%d", code)), 0644)
	os.WriteFile(doneFile, []byte{}, 0644)
}

func appendToFile(path, text string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	io.WriteString(f, text)
}
