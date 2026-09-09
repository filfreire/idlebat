package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/filfreire/idlebat/internal/config"
	"github.com/filfreire/idlebat/internal/state"
)

// setupStateDir creates a minimal state dir with config and prompt for testing.
func setupStateDir(t *testing.T, workDir, stepID string) string {
	t.Helper()
	stateDir := t.TempDir()
	for _, sub := range []string{"prompts", "logs", "status", "jobs"} {
		if err := os.MkdirAll(filepath.Join(stateDir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{
		Name:    "Test",
		WorkDir: workDir,
		Model:   "test-model",
	}
	if err := state.WriteRunConfig(stateDir, cfg); err != nil {
		t.Fatal(err)
	}
	// Use an unsupported provider so tests never invoke a real authenticated CLI.
	job := config.ExpandedStep{
		ID:       stepID,
		Name:     stepID,
		Provider: "test-provider",
		WorkDir:  workDir,
	}
	if err := state.WriteJob(stateDir, job); err != nil {
		t.Fatal(err)
	}
	promptFile := filepath.Join(stateDir, "prompts", stepID+".txt")
	if err := os.WriteFile(promptFile, []byte("test prompt"), 0644); err != nil {
		t.Fatal(err)
	}
	return stateDir
}

func TestRunInternalCreatesWorkDir(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	workDir := filepath.Join(t.TempDir(), "new-project")
	stateDir := setupStateDir(t, workDir, "step1")

	// Will fail at claude invocation, but work dir should be created first.
	RunInternal(stateDir, "step1")

	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		t.Errorf("work dir was not created: %s", workDir)
	}
}

func TestRunInternalCreatesNestedWorkDir(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	workDir := filepath.Join(t.TempDir(), "deep", "nested", "project")
	stateDir := setupStateDir(t, workDir, "step1")

	RunInternal(stateDir, "step1")

	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		t.Errorf("nested work dir was not created: %s", workDir)
	}
}

func TestRunInternalExistingWorkDirPreserved(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	workDir := filepath.Join(t.TempDir(), "existing-project")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(workDir, "marker.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	stateDir := setupStateDir(t, workDir, "step1")
	RunInternal(stateDir, "step1")

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("marker file was removed: %v", err)
	}
	if string(data) != "keep" {
		t.Errorf("marker file content changed: got %q", string(data))
	}
}

func TestRunInternalWritesExitAndDoneFiles(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	workDir := filepath.Join(t.TempDir(), "proj")
	stateDir := setupStateDir(t, workDir, "step1")

	RunInternal(stateDir, "step1")

	exitFile := filepath.Join(stateDir, "status", "step1.exit")
	doneFile := filepath.Join(stateDir, "status", "step1.done")

	if _, err := os.Stat(exitFile); os.IsNotExist(err) {
		t.Error("exit file was not written")
	}
	if _, err := os.Stat(doneFile); os.IsNotExist(err) {
		t.Error("done file was not written")
	}
}

func TestRunInternalBadConfigFails(t *testing.T) {
	stateDir := t.TempDir()
	for _, sub := range []string{"prompts", "logs", "status"} {
		os.MkdirAll(filepath.Join(stateDir, sub), 0755)
	}
	// No config.json written -- should fail reading config.
	code := RunInternal(stateDir, "step1")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRunInternalMissingPromptFails(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	workDir := t.TempDir()
	stateDir := t.TempDir()
	for _, sub := range []string{"prompts", "logs", "status"} {
		os.MkdirAll(filepath.Join(stateDir, sub), 0755)
	}
	cfg := &config.Config{Name: "T", WorkDir: workDir, Model: "m"}
	state.WriteRunConfig(stateDir, cfg)
	// No prompt file written.
	code := RunInternal(stateDir, "no-prompt")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestWriteExitAndDone(t *testing.T) {
	dir := t.TempDir()
	exitFile := filepath.Join(dir, "test.exit")
	doneFile := filepath.Join(dir, "test.done")

	writeExitAndDone(exitFile, doneFile, 42)

	data, err := os.ReadFile(exitFile)
	if err != nil {
		t.Fatalf("reading exit file: %v", err)
	}
	if string(data) != "42" {
		t.Errorf("expected '42', got %q", string(data))
	}

	if _, err := os.Stat(doneFile); os.IsNotExist(err) {
		t.Error("done file not created")
	}
}

func TestAppendToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "append.log")

	appendToFile(path, "line1\n")
	appendToFile(path, "line2\n")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "line1\nline2\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestAppendToFileCreatesNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subdir", "new.log")
	// Parent dir does not exist -- appendToFile silently fails.
	appendToFile(path, "data")
	// Should not panic, just silently skip.
}

func TestLoadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := strings.Join([]string{
		"# comment line",
		"",
		"KEY1=value1",
		"KEY2='quoted value'",
		`KEY3="double quoted"`,
		"KEY4 = spaced ",
		"NOEQUALS",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Clear test keys first
	for _, k := range []string{"KEY1", "KEY2", "KEY3", "KEY4"} {
		os.Unsetenv(k)
	}

	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile failed: %v", err)
	}

	tests := []struct {
		key      string
		expected string
	}{
		{"KEY1", "value1"},
		{"KEY2", "quoted value"},
		{"KEY3", "double quoted"},
		{"KEY4", "spaced"},
	}
	for _, tt := range tests {
		got := os.Getenv(tt.key)
		if got != tt.expected {
			t.Errorf("env %s = %q, want %q", tt.key, got, tt.expected)
		}
	}

	// Clean up
	for _, k := range []string{"KEY1", "KEY2", "KEY3", "KEY4"} {
		os.Unsetenv(k)
	}
}

func TestLoadEnvFileMissing(t *testing.T) {
	err := loadEnvFile(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Error("expected error for missing env file")
	}
}

func TestExtractTextFromStreamLog(t *testing.T) {
	dir := t.TempDir()
	streamLog := filepath.Join(dir, "stream.jsonl")
	logFile := filepath.Join(dir, "output.log")

	lines := strings.Join([]string{
		`{"type":"system","message":{}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello from assistant"}]}}`,
		`{"type":"result","message":{"content":[{"type":"text","text":"Final result"}]}}`,
		`not json at all`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"bash"}]}}`,
	}, "\n")
	if err := os.WriteFile(streamLog, []byte(lines), 0644); err != nil {
		t.Fatal(err)
	}

	extractTextFromStreamLog(streamLog, logFile)

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "Hello from assistant") {
		t.Error("expected assistant text in output")
	}
	if !strings.Contains(content, "Final result") {
		t.Error("expected result text in output")
	}
	if strings.Contains(content, "tool_use") {
		t.Error("tool_use blocks should not appear in output")
	}
}

func TestExtractTextFromStreamLogMissing(t *testing.T) {
	dir := t.TempDir()
	// Should not panic on missing files.
	extractTextFromStreamLog(
		filepath.Join(dir, "missing.jsonl"),
		filepath.Join(dir, "out.log"),
	)
}
