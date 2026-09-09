package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/filfreire/idlebat/internal/config"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Task", "my-task"},
		{"hello world 123", "hello-world-123"},
		{"---", "unnamed"},
		{"", "unnamed"},
		{"UPPERCASE", "uppercase"},
		{"a!@#b", "a-b"},
	}

	for _, tt := range tests {
		got := SanitizeName(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSetupStateDirIsUniqueAndTracksLatest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IDLEBAT_STATE_DIR", root)

	first, err := SetupStateDir("My Workflow")
	if err != nil {
		t.Fatal(err)
	}
	second, err := SetupStateDir("My Workflow")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("state directories collided: %s", first)
	}
	latest, err := LatestStateDir("My Workflow")
	if err != nil {
		t.Fatal(err)
	}
	if latest != second {
		t.Fatalf("latest = %q, want %q", latest, second)
	}
	for _, sub := range []string{"prompts", "logs", "status", "artifacts", "jobs", "worktrees"} {
		if _, err := os.Stat(filepath.Join(second, sub)); err != nil {
			t.Errorf("missing state subdirectory %s: %v", sub, err)
		}
	}
}

func TestJobAndManifestRoundTrip(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "jobs"), 0755); err != nil {
		t.Fatal(err)
	}
	job := config.ExpandedStep{ID: "review", Provider: "codex", Output: "review.md"}
	if err := WriteJob(stateDir, job); err != nil {
		t.Fatal(err)
	}
	gotJob, err := ReadJob(stateDir, "review")
	if err != nil {
		t.Fatal(err)
	}
	if gotJob.Provider != "codex" || gotJob.Output != "review.md" {
		t.Fatalf("unexpected job: %+v", gotJob)
	}

	manifest := Manifest{Name: "Review", RunID: "run-1", GitHead: "abc"}
	if err := WriteManifest(stateDir, manifest); err != nil {
		t.Fatal(err)
	}
	gotManifest, err := ReadManifest(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if gotManifest.RunID != "run-1" || gotManifest.GitHead != "abc" {
		t.Fatalf("unexpected manifest: %+v", gotManifest)
	}
}
