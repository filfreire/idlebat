package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigBasic(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "test.yaml")
	os.WriteFile(configFile, []byte(`
name: "Test Task"
work_dir: .
model: claude-opus-4-6

steps:
  - id: step-one
    name: "First step"
    prompt: |
      Do something simple.
    check_files:
      - output.txt
`), 0644)

	cfg, steps, err := ParseConfig(configFile)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	if cfg.Name != "Test Task" {
		t.Errorf("expected name 'Test Task', got '%s'", cfg.Name)
	}
	if cfg.Model != "claude-opus-4-6" {
		t.Errorf("expected model 'claude-opus-4-6', got '%s'", cfg.Model)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].ID != "step-one" {
		t.Errorf("expected step ID 'step-one', got '%s'", steps[0].ID)
	}
	if steps[0].Name != "First step" {
		t.Errorf("expected step name 'First step', got '%s'", steps[0].Name)
	}
	if len(steps[0].CheckFiles) != 1 || steps[0].CheckFiles[0] != "output.txt" {
		t.Errorf("unexpected check_files: %v", steps[0].CheckFiles)
	}
}

func TestParseConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "test.yaml")
	os.WriteFile(configFile, []byte(`
steps:
  - id: s1
    prompt: "hello"
`), 0644)

	cfg, steps, err := ParseConfig(configFile)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if cfg.Name != "Untitled" {
		t.Errorf("expected default name 'Untitled', got '%s'", cfg.Name)
	}
	if cfg.Model != "claude-sonnet-4-6" {
		t.Errorf("expected default model 'claude-sonnet-4-6', got '%s'", cfg.Model)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
}

func TestParseConfigRepeat(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "test.yaml")
	os.WriteFile(configFile, []byte(`
name: "Repeat Test"
steps:
  - id: review
    name: "Review code"
    repeat: 3
    prompt: |
      Iteration {{iteration}} of {{repeat}} for {{step_id}}.
`), 0644)

	_, steps, err := ParseConfig(configFile)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps after repeat expansion, got %d", len(steps))
	}

	if steps[0].ID != "review-1" {
		t.Errorf("expected ID 'review-1', got '%s'", steps[0].ID)
	}
	if steps[1].ID != "review-2" {
		t.Errorf("expected ID 'review-2', got '%s'", steps[1].ID)
	}
	if steps[2].ID != "review-3" {
		t.Errorf("expected ID 'review-3', got '%s'", steps[2].ID)
	}

	if steps[0].Name != "Review code (iteration 1/3)" {
		t.Errorf("unexpected name: '%s'", steps[0].Name)
	}

	if !containsStr(steps[0].Prompt, "Iteration 1 of 3") {
		t.Errorf("template not expanded in prompt: %s", steps[0].Prompt)
	}
	if !containsStr(steps[1].Prompt, "Iteration 2 of 3") {
		t.Errorf("template not expanded in prompt: %s", steps[1].Prompt)
	}
	if !containsStr(steps[0].Prompt, "review") {
		t.Errorf("step_id template not expanded: %s", steps[0].Prompt)
	}
}

func TestParseConfigPromptFile(t *testing.T) {
	dir := t.TempDir()

	promptContent := "This is the prompt from a file.\n"
	os.WriteFile(filepath.Join(dir, "my-prompt.txt"), []byte(promptContent), 0644)

	configFile := filepath.Join(dir, "test.yaml")
	os.WriteFile(configFile, []byte(`
name: "Prompt File Test"
steps:
  - id: from-file
    name: "Step from file"
    prompt_file: ./my-prompt.txt
`), 0644)

	_, steps, err := ParseConfig(configFile)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Prompt != promptContent {
		t.Errorf("expected prompt from file, got: %s", steps[0].Prompt)
	}
}

func TestParseConfigNoPrompt(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "test.yaml")
	os.WriteFile(configFile, []byte(`
steps:
  - id: no-prompt
    name: "Missing prompt"
`), 0644)

	_, _, err := ParseConfig(configFile)
	if err == nil {
		t.Fatal("expected error for step with no prompt")
	}
}

func TestExpandPath(t *testing.T) {
	if expandPath("/some/path") != "/some/path" {
		t.Error("absolute path should pass through")
	}
	if expandPath("relative/path") != "relative/path" {
		t.Error("relative path should pass through")
	}
	if expandPath("") != "" {
		t.Error("empty string should pass through")
	}

	home, _ := os.UserHomeDir()
	result := expandPath("~/test")
	expected := filepath.Join(home, "test")
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
