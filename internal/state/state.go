package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/filfreire/idlebat/internal/config"
)

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

func SetupStateDir(taskName string) (string, error) {
	sanitized := SanitizeName(taskName)
	stateDir := filepath.Join(os.TempDir(), "idlebat-"+sanitized)

	subdirs := []string{"prompts", "logs", "status"}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(stateDir, sub), 0755); err != nil {
			return "", fmt.Errorf("creating %s: %w", sub, err)
		}
	}

	return stateDir, nil
}

// WriteRunConfig serializes the Config to state dir so the internal runner can read it.
func WriteRunConfig(stateDir string, cfg *config.Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "config.json"), data, 0644)
}

// ReadRunConfig loads Config from the state dir.
func ReadRunConfig(stateDir string) (*config.Config, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, "config.json"))
	if err != nil {
		return nil, err
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
