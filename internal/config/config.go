package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var jobIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// Config holds the resolved top-level workflow configuration.
type Config struct {
	Version     int                 `yaml:"version" json:"version"`
	Name        string              `yaml:"name" json:"name"`
	WorkDir     string              `yaml:"work_dir" json:"work_dir"`
	Model       string              `yaml:"model" json:"model"`
	EnvFile     string              `yaml:"env_file" json:"env_file"`
	MaxParallel int                 `yaml:"max_parallel" json:"max_parallel"`
	Variables   map[string]string   `yaml:"variables" json:"variables"`
	Agents      map[string]AgentDef `yaml:"agents" json:"agents"`
	Legacy      bool                `yaml:"-" json:"legacy"`
}

// AgentDef selects a supported CLI provider and optional provider-specific settings.
type AgentDef struct {
	Provider string   `yaml:"provider" json:"provider"`
	Model    string   `yaml:"model" json:"model"`
	Args     []string `yaml:"args" json:"args"`
}

// StepDef is a raw step or job definition from the YAML file.
type StepDef struct {
	ID         string            `yaml:"id"`
	Name       string            `yaml:"name"`
	Agent      string            `yaml:"agent"`
	Model      string            `yaml:"model"`
	Prompt     string            `yaml:"prompt"`
	PromptFile string            `yaml:"prompt_file"`
	Repeat     int               `yaml:"repeat"`
	Needs      []string          `yaml:"needs"`
	Output     string            `yaml:"output"`
	Isolation  string            `yaml:"isolation"`
	Variables  map[string]string `yaml:"variables"`
	When       map[string]string `yaml:"when"`
	CheckFiles []string          `yaml:"check_files"`
}

// ExpandedStep is an executable job after repeat expansion and prompt resolution.
type ExpandedStep struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Agent      string            `json:"agent"`
	Provider   string            `json:"provider"`
	Model      string            `json:"model"`
	Args       []string          `json:"args"`
	Prompt     string            `json:"prompt"`
	Needs      []string          `json:"needs"`
	Output     string            `json:"output"`
	Isolation  string            `json:"isolation"`
	Variables  map[string]string `json:"variables"`
	CheckFiles []string          `json:"check_files"`
	WorkDir    string            `json:"work_dir,omitempty"`
}

type rawConfig struct {
	Version     int                 `yaml:"version"`
	Name        string              `yaml:"name"`
	WorkDir     string              `yaml:"work_dir"`
	Model       string              `yaml:"model"`
	EnvFile     string              `yaml:"env_file"`
	MaxParallel int                 `yaml:"max_parallel"`
	Variables   map[string]string   `yaml:"variables"`
	Agents      map[string]AgentDef `yaml:"agents"`
	Steps       []StepDef           `yaml:"steps"`
	Jobs        []StepDef           `yaml:"jobs"`
}

// ParseOptions contains command-line overrides applied after loading YAML.
type ParseOptions struct {
	Variables map[string]string
	WorkDir   string
}

func ParseConfig(configFile string) (*Config, []ExpandedStep, error) {
	return ParseConfigWithOptions(configFile, ParseOptions{})
}

func ParseConfigWithOptions(configFile string, opts ParseOptions) (*Config, []ExpandedStep, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, nil, fmt.Errorf("reading config: %w", err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parsing YAML: %w", err)
	}
	if len(raw.Steps) > 0 && len(raw.Jobs) > 0 {
		return nil, nil, fmt.Errorf("config cannot contain both steps and jobs")
	}
	if len(raw.Steps) == 0 && len(raw.Jobs) == 0 {
		return nil, nil, fmt.Errorf("config has no steps or jobs")
	}

	configDir, err := filepath.Abs(filepath.Dir(configFile))
	if err != nil {
		return nil, nil, fmt.Errorf("resolving config dir: %w", err)
	}

	variables := copyMap(raw.Variables)
	for key, value := range opts.Variables {
		variables[key] = value
	}

	workDir := replaceVariables(raw.WorkDir, variables)
	if opts.WorkDir != "" {
		workDir = opts.WorkDir
	}
	cfg := &Config{
		Version:     raw.Version,
		Name:        replaceVariables(raw.Name, variables),
		WorkDir:     expandPath(workDir),
		Model:       raw.Model,
		EnvFile:     expandPath(replaceVariables(raw.EnvFile, variables)),
		MaxParallel: raw.MaxParallel,
		Variables:   variables,
		Agents:      raw.Agents,
		Legacy:      len(raw.Steps) > 0,
	}
	if cfg.Version == 0 {
		cfg.Version = 1
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
	if !filepath.IsAbs(cfg.WorkDir) {
		cfg.WorkDir = filepath.Join(configDir, cfg.WorkDir)
	}
	if cfg.EnvFile != "" && !filepath.IsAbs(cfg.EnvFile) {
		cfg.EnvFile = filepath.Join(configDir, cfg.EnvFile)
	}
	if cfg.MaxParallel < 1 {
		if cfg.Legacy {
			cfg.MaxParallel = 1
		} else {
			cfg.MaxParallel = 3
		}
	}

	defs := raw.Jobs
	if cfg.Legacy {
		defs = raw.Steps
	}
	steps, err := expandSteps(defs, cfg, configDir)
	if err != nil {
		return nil, nil, err
	}
	if len(steps) == 0 {
		return nil, nil, fmt.Errorf("config has no jobs matching the current variables")
	}
	if err := validateGraph(steps); err != nil {
		return nil, nil, err
	}
	if err := validateTemplates(cfg, steps); err != nil {
		return nil, nil, err
	}
	return cfg, steps, nil
}

func expandSteps(defs []StepDef, cfg *Config, configDir string) ([]ExpandedStep, error) {
	var steps []ExpandedStep
	previousID := ""
	for i, sd := range defs {
		if sd.ID == "" {
			sd.ID = fmt.Sprintf("step-%d", i)
		}
		if !jobIDPattern.MatchString(sd.ID) {
			return nil, fmt.Errorf("invalid job id %q: use letters, numbers, '-' or '_'", sd.ID)
		}
		if sd.Name == "" {
			sd.Name = sd.ID
		}
		if !conditionsMatch(sd.When, cfg.Variables) {
			continue
		}
		repeat := sd.Repeat
		if repeat < 1 {
			repeat = 1
		}

		promptText := sd.Prompt
		if sd.PromptFile != "" {
			pf := expandPath(replaceVariables(sd.PromptFile, cfg.Variables))
			if !filepath.IsAbs(pf) {
				pf = filepath.Join(configDir, pf)
			}
			data, err := os.ReadFile(pf)
			if err != nil {
				return nil, fmt.Errorf("reading prompt_file for job %q: %w", sd.ID, err)
			}
			promptText = string(data)
		}
		if promptText == "" {
			return nil, fmt.Errorf("job %q has no prompt or prompt_file", sd.ID)
		}

		agentName := sd.Agent
		if agentName == "" {
			agentName = "claude"
		}
		agent, err := resolveAgent(agentName, cfg)
		if err != nil {
			return nil, fmt.Errorf("job %q: %w", sd.ID, err)
		}
		if sd.Model != "" {
			agent.Model = sd.Model
		}

		for iter := 1; iter <= repeat; iter++ {
			effectiveID := sd.ID
			effectiveName := sd.Name
			if repeat > 1 {
				effectiveID = fmt.Sprintf("%s-%d", sd.ID, iter)
				effectiveName = fmt.Sprintf("%s (iteration %d/%d)", sd.Name, iter, repeat)
			}

			vars := copyMap(cfg.Variables)
			for key, value := range sd.Variables {
				vars[key] = replaceVariables(value, cfg.Variables)
			}
			vars["iteration"] = strconv.Itoa(iter)
			vars["repeat"] = strconv.Itoa(repeat)
			vars["step_id"] = sd.ID
			vars["step_name"] = sd.Name
			vars["job_id"] = effectiveID
			vars["job_name"] = effectiveName

			needs := append([]string(nil), sd.Needs...)
			if cfg.Legacy && previousID != "" {
				needs = []string{previousID}
			}
			isolation := sd.Isolation
			if isolation == "" {
				isolation = "shared"
			}
			if isolation != "shared" && isolation != "git-worktree" {
				return nil, fmt.Errorf("job %q has unsupported isolation %q", sd.ID, isolation)
			}

			steps = append(steps, ExpandedStep{
				ID:         effectiveID,
				Name:       effectiveName,
				Agent:      agentName,
				Provider:   agent.Provider,
				Model:      agent.Model,
				Args:       append([]string(nil), agent.Args...),
				Prompt:     replaceVariables(promptText, vars),
				Needs:      needs,
				Output:     replaceVariables(sd.Output, vars),
				Isolation:  isolation,
				Variables:  vars,
				CheckFiles: append([]string(nil), sd.CheckFiles...),
			})
			previousID = effectiveID
		}
	}
	return steps, nil
}

func conditionsMatch(conditions, variables map[string]string) bool {
	for key, expected := range conditions {
		actual, ok := variables[key]
		if !ok || actual != replaceVariables(expected, variables) {
			return false
		}
	}
	return true
}

func resolveAgent(name string, cfg *Config) (AgentDef, error) {
	if def, ok := cfg.Agents[name]; ok {
		if def.Provider == "" {
			def.Provider = name
		}
		if def.Model == "" && def.Provider == "claude" {
			def.Model = cfg.Model
		}
		if !supportedProvider(def.Provider) {
			return AgentDef{}, fmt.Errorf("agent %q uses unsupported provider %q", name, def.Provider)
		}
		return def, nil
	}
	if !supportedProvider(name) {
		return AgentDef{}, fmt.Errorf("unknown agent %q", name)
	}
	def := AgentDef{Provider: name}
	if name == "claude" {
		def.Model = cfg.Model
	}
	return def, nil
}

func supportedProvider(provider string) bool {
	return provider == "claude" || provider == "codex" || provider == "cursor"
}

func validateGraph(steps []ExpandedStep) error {
	byID := make(map[string]ExpandedStep, len(steps))
	for _, step := range steps {
		if _, exists := byID[step.ID]; exists {
			return fmt.Errorf("duplicate job id %q", step.ID)
		}
		if step.Output != "" {
			clean := filepath.Clean(step.Output)
			if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return fmt.Errorf("job %q output must be relative to the run artifacts directory", step.ID)
			}
		}
		byID[step.ID] = step
	}
	for _, step := range steps {
		for _, need := range step.Needs {
			if need == step.ID {
				return fmt.Errorf("job %q cannot depend on itself", step.ID)
			}
			if _, ok := byID[need]; !ok {
				return fmt.Errorf("job %q needs unknown job %q", step.ID, need)
			}
		}
	}

	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("dependency cycle includes job %q", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, need := range byID[id].Needs {
			if err := visit(need); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func validateTemplates(cfg *Config, steps []ExpandedStep) error {
	if strings.Contains(cfg.Name, "{{") {
		return fmt.Errorf("workflow name contains an unresolved template variable")
	}
	if strings.Contains(cfg.WorkDir, "{{") {
		return fmt.Errorf("work_dir contains an unresolved template variable")
	}
	knownOutputs := map[string]bool{}
	for _, step := range steps {
		knownOutputs["output."+step.ID] = true
		knownOutputs["outputs."+step.ID] = true
	}
	templatePattern := regexp.MustCompile(`\{\{([^{}]+)\}\}`)
	for _, step := range steps {
		for _, match := range templatePattern.FindAllStringSubmatch(step.Prompt, -1) {
			key := match[1]
			if knownOutputs[key] || key == "artifacts_dir" || key == "state_dir" || key == "work_dir" {
				continue
			}
			return fmt.Errorf("job %q uses unresolved template variable %q", step.ID, key)
		}
	}
	return nil
}

// RenderRuntimeVariables resolves output paths and other values known only at run time.
func RenderRuntimeVariables(text string, values map[string]string) string {
	return replaceVariables(text, values)
}

func replaceVariables(text string, variables map[string]string) string {
	for key, value := range variables {
		text = strings.ReplaceAll(text, "{{"+key+"}}", value)
	}
	return text
}

func copyMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
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
