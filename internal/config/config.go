package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the top-level YAML configuration.
type Config struct {
	Name    string `yaml:"name" json:"name"`
	WorkDir string `yaml:"work_dir" json:"work_dir"`
	Model   string `yaml:"model" json:"model"`
	EnvFile string `yaml:"env_file" json:"env_file"`
}

// StepDef is a raw step definition from the YAML file.
type StepDef struct {
	ID         string   `yaml:"id"`
	Name       string   `yaml:"name"`
	Prompt     string   `yaml:"prompt"`
	PromptFile string   `yaml:"prompt_file"`
	Repeat     int      `yaml:"repeat"`
	CheckFiles []string `yaml:"check_files"`
}

// ExpandedStep is a step after repeat expansion and prompt resolution.
type ExpandedStep struct {
	ID         string
	Name       string
	Prompt     string
	CheckFiles []string
}

type rawConfig struct {
	Name    string    `yaml:"name"`
	WorkDir string    `yaml:"work_dir"`
	Model   string    `yaml:"model"`
	EnvFile string    `yaml:"env_file"`
	Steps   []StepDef `yaml:"steps"`
}

func ParseConfig(configFile string) (*Config, []ExpandedStep, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, nil, fmt.Errorf("reading config: %w", err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parsing YAML: %w", err)
	}

	configDir := filepath.Dir(configFile)

	cfg := &Config{
		Name:    raw.Name,
		WorkDir: expandPath(raw.WorkDir),
		Model:   raw.Model,
		EnvFile: expandPath(raw.EnvFile),
	}

	if cfg.Name == "" {
		cfg.Name = "Untitled"
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "."
	}
	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-6"
	}

	// Make work_dir absolute if relative
	if !filepath.IsAbs(cfg.WorkDir) {
		cfg.WorkDir = filepath.Join(configDir, cfg.WorkDir)
	}

	var steps []ExpandedStep
	for i, sd := range raw.Steps {
		if sd.ID == "" {
			sd.ID = fmt.Sprintf("step-%d", i)
		}
		if sd.Name == "" {
			sd.Name = sd.ID
		}
		repeat := sd.Repeat
		if repeat < 1 {
			repeat = 1
		}

		// Resolve prompt text
		promptText := sd.Prompt
		if sd.PromptFile != "" {
			pf := expandPath(sd.PromptFile)
			if !filepath.IsAbs(pf) {
				pf = filepath.Join(configDir, pf)
			}
			data, err := os.ReadFile(pf)
			if err != nil {
				return nil, nil, fmt.Errorf("reading prompt_file for step '%s': %w", sd.ID, err)
			}
			promptText = string(data)
		}

		if promptText == "" {
			return nil, nil, fmt.Errorf("step '%s' has no prompt or prompt_file", sd.ID)
		}

		for iter := 1; iter <= repeat; iter++ {
			effectiveID := sd.ID
			effectiveName := sd.Name
			if repeat > 1 {
				effectiveID = fmt.Sprintf("%s-%d", sd.ID, iter)
				effectiveName = fmt.Sprintf("%s (iteration %d/%d)", sd.Name, iter, repeat)
			}

			// Template substitution
			p := promptText
			p = strings.ReplaceAll(p, "{{iteration}}", fmt.Sprintf("%d", iter))
			p = strings.ReplaceAll(p, "{{repeat}}", fmt.Sprintf("%d", repeat))
			p = strings.ReplaceAll(p, "{{step_id}}", sd.ID)
			p = strings.ReplaceAll(p, "{{step_name}}", sd.Name)

			steps = append(steps, ExpandedStep{
				ID:         effectiveID,
				Name:       effectiveName,
				Prompt:     p,
				CheckFiles: sd.CheckFiles,
			})
		}
	}

	return cfg, steps, nil
}

func expandPath(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[1:])
	}
	return p
}
