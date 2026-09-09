package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/filfreire/idlebat/internal/config"
	"github.com/filfreire/idlebat/internal/state"
)

func TestRunFansOutAndConsolidatesArtifacts(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFakeProvider(t, binDir, "claude")
	writeFakeProvider(t, binDir, "codex")
	writeFakeProvider(t, binDir, "agent")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("IDLEBAT_STATE_DIR", filepath.Join(root, "state"))

	repoDir := filepath.Join(root, "repo")
	initGitRepo(t, repoDir)
	stateDir, err := state.SetupStateDir("workflow-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_ARTIFACT_ROOT", filepath.Join(stateDir, "artifacts"))

	cfg := &config.Config{Name: "test", WorkDir: repoDir, MaxParallel: 3}
	if err := state.WriteRunConfig(stateDir, cfg); err != nil {
		t.Fatal(err)
	}
	jobs := []config.ExpandedStep{
		{ID: "a", Name: "A", Agent: "claude", Provider: "claude", Prompt: "REVIEW", Output: "findings/A.md", Isolation: "git-worktree"},
		{ID: "b", Name: "B", Agent: "codex", Provider: "codex", Prompt: "REVIEW", Output: "findings/B.md", Isolation: "git-worktree"},
		{ID: "c", Name: "C", Agent: "cursor", Provider: "cursor", Prompt: "REVIEW", Output: "findings/C.md", Isolation: "git-worktree"},
		{ID: "consolidate", Name: "Consolidate", Agent: "codex", Provider: "codex", Needs: []string{"a", "b", "c"}, Prompt: "CONSOLIDATE {{outputs.a}} {{outputs.b}} {{outputs.c}}", Output: "review.md", Isolation: "shared"},
	}

	results, err := Run(cfg, jobs, stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("got %d results", len(results))
	}
	for _, result := range results {
		if result.ExitCode != 0 {
			t.Fatalf("job failed: %+v", result)
		}
	}
	for _, path := range []string{"findings/A.md", "findings/B.md", "findings/C.md", "review.md"} {
		data, err := os.ReadFile(filepath.Join(stateDir, "artifacts", path))
		if err != nil {
			t.Fatalf("reading artifact %s: %v", path, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Errorf("artifact %s is empty", path)
		}
	}
	if _, err := os.Stat(filepath.Join(stateDir, "worktrees", "a")); !os.IsNotExist(err) {
		t.Errorf("isolated worktree was not removed")
	}
}

func TestRunBlocksDependentJobAfterFailure(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexit 7\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("IDLEBAT_STATE_DIR", filepath.Join(root, "state"))
	stateDir, err := state.SetupStateDir("failure-test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Name: "test", WorkDir: root, MaxParallel: 2}
	if err := state.WriteRunConfig(stateDir, cfg); err != nil {
		t.Fatal(err)
	}
	jobs := []config.ExpandedStep{
		{ID: "fail", Provider: "claude", Prompt: "fail"},
		{ID: "blocked", Provider: "claude", Prompt: "never", Needs: []string{"fail"}},
	}
	results, err := Run(cfg, jobs, stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].ExitCode != 7 || !results[1].Blocked {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func writeFakeProvider(t *testing.T, binDir, name string) {
	t.Helper()
	script := `#!/bin/sh
name=$(basename "$0")
prompt="$*"
if [ "$name" != "agent" ]; then
  prompt=$(cat)
fi
if printf '%s' "$prompt" | grep -q CONSOLIDATE; then
  test -f "$TEST_ARTIFACT_ROOT/findings/A.md" || exit 21
  test -f "$TEST_ARTIFACT_ROOT/findings/B.md" || exit 22
  test -f "$TEST_ARTIFACT_ROOT/findings/C.md" || exit 23
fi
case "$name" in
  codex) printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"codex result"}}' ;;
  *) printf '%s\n' '{"type":"result","result":"agent result"}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"init", "-q"},
		{"config", "user.email", "idlebat@example.invalid"},
		{"config", "user.name", "idlebat test"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-qm", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
}
