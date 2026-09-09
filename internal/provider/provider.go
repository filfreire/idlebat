package provider

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/filfreire/idlebat/internal/config"
)

func BinaryName(providerName string) (string, error) {
	switch providerName {
	case "claude":
		return "claude", nil
	case "codex":
		return "codex", nil
	case "cursor":
		return "agent", nil
	default:
		return "", fmt.Errorf("unsupported provider %q", providerName)
	}
}

// Version verifies that a provider CLI is installed and returns its version line.
func Version(providerName string) (string, error) {
	binary, err := BinaryName(providerName)
	if err != nil {
		return "", err
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("%s provider requires %q on PATH", providerName, binary)
	}
	output, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("checking %s version: %w: %s", providerName, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// BuildCommand creates a non-interactive CLI process for a supported provider.
func BuildCommand(step config.ExpandedStep, workDir, artifactsDir, prompt string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	switch step.Provider {
	case "claude":
		args := []string{
			"-p",
			"--verbose",
			"--dangerously-skip-permissions",
			"--output-format", "stream-json",
			"--add-dir", artifactsDir,
		}
		if step.Model != "" {
			args = append(args, "--model", step.Model)
		}
		args = append(args, step.Args...)
		cmd = exec.Command("claude", args...)
		cmd.Stdin = strings.NewReader(prompt)
	case "codex":
		args := []string{
			"exec",
			"--json",
			"--ephemeral",
			"--dangerously-bypass-approvals-and-sandbox",
			"--cd", workDir,
			"--add-dir", artifactsDir,
		}
		if step.Model != "" {
			args = append(args, "--model", step.Model)
		}
		args = append(args, step.Args...)
		args = append(args, "-")
		cmd = exec.Command("codex", args...)
		cmd.Stdin = strings.NewReader(prompt)
	case "cursor":
		args := []string{
			"-p",
			"--output-format", "stream-json",
			"--force",
			"--sandbox", "disabled",
			"--approve-mcps",
			"--trust",
			"--workspace", workDir,
			"--add-dir", artifactsDir,
		}
		if step.Model != "" {
			args = append(args, "--model", step.Model)
		}
		args = append(args, step.Args...)
		cmd = exec.Command("agent", args...)
		cmd.Stdin = strings.NewReader(prompt)
	default:
		return nil, fmt.Errorf("unsupported provider %q", step.Provider)
	}
	cmd.Dir = workDir
	return cmd, nil
}

// FinalText extracts a candidate final response from one JSONL event.
func FinalText(providerName, line string) string {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return ""
	}
	if providerName == "codex" {
		return codexText(obj)
	}
	return claudeStyleText(obj)
}

func codexText(obj map[string]interface{}) string {
	if obj["type"] != "item.completed" {
		return ""
	}
	item, ok := obj["item"].(map[string]interface{})
	if !ok || item["type"] != "agent_message" {
		return ""
	}
	text, _ := item["text"].(string)
	return strings.TrimSpace(text)
}

func claudeStyleText(obj map[string]interface{}) string {
	msgType, _ := obj["type"].(string)
	if msgType == "result" {
		if result, ok := obj["result"].(string); ok {
			return strings.TrimSpace(result)
		}
	}
	if msgType != "assistant" && msgType != "result" {
		return ""
	}
	msg, ok := obj["message"].(map[string]interface{})
	if !ok {
		return ""
	}
	content, ok := msg["content"].([]interface{})
	if !ok {
		return ""
	}
	var texts []string
	for _, rawBlock := range content {
		block, ok := rawBlock.(map[string]interface{})
		if !ok || block["type"] != "text" {
			continue
		}
		if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
			texts = append(texts, strings.TrimSpace(text))
		}
	}
	return strings.Join(texts, "\n")
}
