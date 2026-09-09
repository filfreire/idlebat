package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/filfreire/idlebat/internal/config"
)

// Manifest identifies the exact workflow and repository state for a run.
type Manifest struct {
	Name       string            `json:"name"`
	RunID      string            `json:"run_id"`
	ConfigFile string            `json:"config_file"`
	ConfigHash string            `json:"config_hash"`
	WorkDir    string            `json:"work_dir"`
	GitHead    string            `json:"git_head,omitempty"`
	Providers  map[string]string `json:"providers,omitempty"`
	StartedAt  time.Time         `json:"started_at"`
}

func SanitizeName(name string) string {
	name = strings.ToLower(name)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	name = re.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "unnamed"
	}
	return name
}

// Root returns the persistent idlebat state directory.
func Root() (string, error) {
	if root := os.Getenv("IDLEBAT_STATE_DIR"); root != "" {
		return filepath.Abs(root)
	}
	if root := os.Getenv("XDG_STATE_HOME"); root != "" {
		return filepath.Join(root, "idlebat"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "idlebat"), nil
}

// SetupStateDir creates a unique persistent run directory and records it as latest.
func SetupStateDir(taskName string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	workflowDir := filepath.Join(root, SanitizeName(taskName))
	runID := time.Now().UTC().Format("20060102T150405.000000000Z") + fmt.Sprintf("-%d", os.Getpid())
	stateDir := filepath.Join(workflowDir, runID)

	for _, sub := range []string{"prompts", "logs", "status", "artifacts", "jobs", "worktrees"} {
		if err := os.MkdirAll(filepath.Join(stateDir, sub), 0755); err != nil {
			return "", fmt.Errorf("creating %s: %w", sub, err)
		}
	}
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return "", fmt.Errorf("creating workflow state directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "latest"), []byte(stateDir), 0644); err != nil {
		return "", fmt.Errorf("recording latest run: %w", err)
	}
	return stateDir, nil
}

// LatestStateDir returns the most recently created run for a workflow.
func LatestStateDir(taskName string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(root, SanitizeName(taskName), "latest"))
	if err != nil {
		return "", fmt.Errorf("finding latest run: %w", err)
	}
	stateDir := strings.TrimSpace(string(data))
	if stateDir == "" {
		return "", fmt.Errorf("latest run path is empty")
	}
	return stateDir, nil
}

// WriteRunConfig serializes the Config so a child runner can read it.
func WriteRunConfig(stateDir string, cfg *config.Config) error {
	return writeJSON(filepath.Join(stateDir, "config.json"), cfg)
}

func ReadRunConfig(stateDir string) (*config.Config, error) {
	var cfg config.Config
	if err := readJSON(filepath.Join(stateDir, "config.json"), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func WriteJob(stateDir string, job config.ExpandedStep) error {
	return writeJSON(filepath.Join(stateDir, "jobs", job.ID+".json"), job)
}

func ReadJob(stateDir, jobID string) (*config.ExpandedStep, error) {
	var job config.ExpandedStep
	if err := readJSON(filepath.Join(stateDir, "jobs", jobID+".json"), &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func WriteManifest(stateDir string, manifest Manifest) error {
	return writeJSON(filepath.Join(stateDir, "manifest.json"), manifest)
}

func ReadManifest(stateDir string) (*Manifest, error) {
	var manifest Manifest
	if err := readJSON(filepath.Join(stateDir, "manifest.json"), &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func writeJSON(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readJSON(path string, value interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
