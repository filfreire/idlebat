package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/filfreire/idlebat/internal/config"
	"github.com/filfreire/idlebat/internal/display"
	"github.com/filfreire/idlebat/internal/provider"
	"github.com/filfreire/idlebat/internal/runner"
	"github.com/filfreire/idlebat/internal/state"
	"github.com/filfreire/idlebat/internal/workflow"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		return 1
	}

	var configFile, workDirOverride string
	var dryRun, resume bool
	var maxParallel int
	variables := map[string]string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--version":
			fmt.Println("idlebat " + version)
			return 0
		case "--help", "-h":
			printUsage()
			return 0
		case "--internal-run":
			if i+2 >= len(args) {
				fmt.Fprintln(os.Stderr, "Usage: idlebat --internal-run <state-dir> <job-id>")
				return 1
			}
			return runner.RunInternal(args[i+1], args[i+2])
		case "--dry-run":
			dryRun = true
		case "--resume":
			resume = true
		case "--var":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "Error: --var requires KEY=VALUE")
				return 1
			}
			key, value, ok := strings.Cut(args[i], "=")
			if !ok || strings.TrimSpace(key) == "" {
				fmt.Fprintln(os.Stderr, "Error: --var requires KEY=VALUE")
				return 1
			}
			variables[strings.TrimSpace(key)] = value
		case "--work-dir":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "Error: --work-dir requires a path")
				return 1
			}
			workDirOverride = args[i]
		case "--max-parallel":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "Error: --max-parallel requires a positive integer")
				return 1
			}
			value, err := strconv.Atoi(args[i])
			if err != nil || value < 1 {
				fmt.Fprintln(os.Stderr, "Error: --max-parallel requires a positive integer")
				return 1
			}
			maxParallel = value
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", args[i])
				return 1
			}
			if configFile != "" {
				fmt.Fprintf(os.Stderr, "Unexpected argument: %s\n", args[i])
				return 1
			}
			configFile = args[i]
		}
	}

	if configFile == "" {
		fmt.Fprintln(os.Stderr, "Error: config file required")
		printUsage()
		return 1
	}
	absConfigFile, err := filepath.Abs(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving config: %s\n", err)
		return 1
	}
	cfg, jobs, err := config.ParseConfigWithOptions(absConfigFile, config.ParseOptions{
		Variables: variables,
		WorkDir:   workDirOverride,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return 1
	}
	if maxParallel > 0 {
		cfg.MaxParallel = maxParallel
	}

	display.Header(fmt.Sprintf("idlebat - %s", cfg.Name))
	display.LogInfo(fmt.Sprintf("Config: %s", absConfigFile))
	display.LogInfo(fmt.Sprintf("Work dir: %s", cfg.WorkDir))
	display.LogInfo(fmt.Sprintf("Jobs: %d (max parallel: %d)", len(jobs), cfg.MaxParallel))

	if dryRun {
		display.LogInfo("DRY RUN - previewing jobs only")
		display.Divider()
		for i, job := range jobs {
			fmt.Fprintf(os.Stderr, "\n%s Job %d: %s (id: %s, agent: %s/%s)\n",
				display.ColorBold("#"), i+1, job.Name, job.ID, job.Agent, job.Provider)
			if job.Model != "" {
				fmt.Fprintf(os.Stderr, "  model: %s\n", job.Model)
			}
			if len(job.Needs) > 0 {
				fmt.Fprintf(os.Stderr, "  needs: %s\n", strings.Join(job.Needs, ", "))
			}
			if job.Output != "" {
				fmt.Fprintf(os.Stderr, "  output: %s\n", job.Output)
			}
			fmt.Fprintf(os.Stderr, "%s\n", display.PromptPreview(job.Prompt, 5))
		}
		fmt.Fprintln(os.Stderr)
		return 0
	}

	configHash, err := effectiveConfigHash(absConfigFile, variables, workDirOverride, cfg.MaxParallel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error hashing config: %s\n", err)
		return 1
	}
	providerVersions, err := checkProviders(jobs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return 1
	}
	gitHead := currentGitHead(cfg.WorkDir)
	var stateDir string
	if resume {
		stateDir, err = state.LatestStateDir(cfg.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return 1
		}
		manifest, err := state.ReadManifest(stateDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading resume manifest: %s\n", err)
			return 1
		}
		if manifest.ConfigHash != configHash {
			fmt.Fprintln(os.Stderr, "Error: cannot resume because the effective workflow configuration changed")
			return 1
		}
		if manifest.GitHead != gitHead {
			fmt.Fprintf(os.Stderr, "Error: cannot resume because git HEAD changed (%s -> %s)\n", manifest.GitHead, gitHead)
			return 1
		}
	} else {
		stateDir, err = state.SetupStateDir(cfg.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error setting up state dir: %s\n", err)
			return 1
		}
		manifest := state.Manifest{
			Name:       cfg.Name,
			RunID:      filepath.Base(stateDir),
			ConfigFile: absConfigFile,
			ConfigHash: configHash,
			WorkDir:    cfg.WorkDir,
			GitHead:    gitHead,
			Providers:  providerVersions,
			StartedAt:  time.Now().UTC(),
		}
		if err := state.WriteManifest(stateDir, manifest); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing manifest: %s\n", err)
			return 1
		}
	}
	display.LogInfo(fmt.Sprintf("State dir: %s", stateDir))
	if err := state.WriteRunConfig(stateDir, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing run config: %s\n", err)
		return 1
	}

	display.Divider()
	results, runErr := workflow.Run(cfg, jobs, stateDir, resume)
	if runErr != nil {
		display.LogErr(runErr.Error())
	}
	printSummary(results, stateDir)
	if runErr != nil {
		return 1
	}
	for _, result := range results {
		if result.ExitCode != 0 {
			return 1
		}
	}
	return 0
}

func printSummary(results []workflow.Result, stateDir string) {
	display.Header("Summary")
	failed := 0
	for _, result := range results {
		status := display.ColorGreen("PASS")
		if result.Skipped {
			status = display.ColorDim("SKIP")
		} else if result.Blocked {
			status = display.ColorRed("BLOCK")
			failed++
		} else if result.ExitCode != 0 {
			status = display.ColorRed("FAIL")
			failed++
		}
		fmt.Fprintf(os.Stderr, "  %s %s [%s] %s\n", status, result.ID, result.Provider, display.FormatDuration(result.Duration))
	}
	fmt.Fprintln(os.Stderr)
	if failed > 0 {
		display.LogErr(fmt.Sprintf("%d job(s) failed or were blocked", failed))
	} else {
		display.LogSuccess("All jobs passed")
	}
	display.LogInfo(fmt.Sprintf("Artifacts: %s", filepath.Join(stateDir, "artifacts")))
}

func effectiveConfigHash(path string, variables map[string]string, workDir string, maxParallel int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write(data)
	keys := make([]string, 0, len(variables))
	for key := range variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, _ = fmt.Fprintf(hash, "\nvar:%s=%s", key, variables[key])
	}
	_, _ = fmt.Fprintf(hash, "\nwork_dir:%s\nmax_parallel:%d", workDir, maxParallel)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func currentGitHead(workDir string) string {
	cmd := exec.Command("git", "-C", workDir, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func checkProviders(jobs []config.ExpandedStep) (map[string]string, error) {
	versions := map[string]string{}
	for _, job := range jobs {
		if _, checked := versions[job.Provider]; checked {
			continue
		}
		version, err := provider.Version(job.Provider)
		if err != nil {
			return nil, err
		}
		versions[job.Provider] = version
		display.LogInfo(fmt.Sprintf("Provider %s: %s", job.Provider, version))
	}
	return versions, nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: idlebat <config.yaml> [flags]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --dry-run              Preview jobs without executing")
	fmt.Fprintln(os.Stderr, "  --resume               Resume the latest matching run")
	fmt.Fprintln(os.Stderr, "  --var KEY=VALUE        Set a template variable (repeatable)")
	fmt.Fprintln(os.Stderr, "  --work-dir PATH        Override the configured working directory")
	fmt.Fprintln(os.Stderr, "  --max-parallel N       Override maximum concurrent jobs")
	fmt.Fprintln(os.Stderr, "  --version              Print version and exit")
	fmt.Fprintln(os.Stderr, "  --help                 Show this help")
}
