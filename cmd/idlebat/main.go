package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/filfreire/idlebat/internal/config"
	"github.com/filfreire/idlebat/internal/display"
	"github.com/filfreire/idlebat/internal/monitor"
	"github.com/filfreire/idlebat/internal/runner"
	"github.com/filfreire/idlebat/internal/state"
	"github.com/filfreire/idlebat/internal/terminal"
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

	// Parse flags
	var configFile string
	var dryRun, resume bool
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
				fmt.Fprintln(os.Stderr, "Usage: idlebat --internal-run <state-dir> <step-id>")
				return 1
			}
			return runner.RunInternal(args[i+1], args[i+2])
		case "--dry-run":
			dryRun = true
		case "--resume":
			resume = true
		default:
			if args[i][0] == '-' {
				fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", args[i])
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

	cfg, steps, err := config.ParseConfig(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return 1
	}

	display.Header(fmt.Sprintf("idlebat - %s", cfg.Name))
	display.LogInfo(fmt.Sprintf("Config: %s", configFile))
	display.LogInfo(fmt.Sprintf("Work dir: %s", cfg.WorkDir))
	display.LogInfo(fmt.Sprintf("Model: %s", cfg.Model))
	display.LogInfo(fmt.Sprintf("Steps: %d", len(steps)))

	if dryRun {
		display.LogInfo("DRY RUN - previewing steps only")
		display.Divider()
		for i, step := range steps {
			fmt.Fprintf(os.Stderr, "\n%s Step %d: %s (id: %s)\n",
				display.ColorBold("#"), i+1, step.Name, step.ID)
			fmt.Fprintf(os.Stderr, "%s\n", display.PromptPreview(step.Prompt, 5))
			if len(step.CheckFiles) > 0 {
				fmt.Fprintf(os.Stderr, "  check_files: %s\n",
					display.JoinStrings(step.CheckFiles, ", "))
			}
		}
		fmt.Fprintln(os.Stderr)
		return 0
	}

	stateDir, err := state.SetupStateDir(cfg.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up state dir: %s\n", err)
		return 1
	}
	display.LogInfo(fmt.Sprintf("State dir: %s", stateDir))

	if err := state.WriteRunConfig(stateDir, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing run config: %s\n", err)
		return 1
	}

	spawner := terminal.NewSpawner()

	type stepResult struct {
		ID       string
		Name     string
		ExitCode int
		Duration time.Duration
		Skipped  bool
	}
	var results []stepResult

	display.Divider()

	for i, step := range steps {
		fmt.Fprintln(os.Stderr)
		display.LogInfo(fmt.Sprintf("Step %d/%d: %s", i+1, len(steps), step.Name))

		doneFile := filepath.Join(stateDir, "status", step.ID+".done")

		// Resume: skip completed steps
		if resume {
			if _, err := os.Stat(doneFile); err == nil {
				display.LogSuccess(fmt.Sprintf("Skipping (already done): %s", step.ID))
				results = append(results, stepResult{
					ID:      step.ID,
					Name:    step.Name,
					Skipped: true,
				})
				continue
			}
		}

		// Write prompt file
		promptFile := filepath.Join(stateDir, "prompts", step.ID+".txt")
		if err := os.WriteFile(promptFile, []byte(step.Prompt), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing prompt: %s\n", err)
			return 1
		}

		// Clean stale status files from previous runs
		os.Remove(filepath.Join(stateDir, "status", step.ID+".done"))
		os.Remove(filepath.Join(stateDir, "status", step.ID+".exit"))

		stepStart := time.Now()

		// Spawn terminal for step
		if err := spawner.SpawnStep(step.ID, step.Name, stateDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error spawning terminal: %s\n", err)
			return 1
		}
		display.LogSuccess("Terminal spawned")

		// Monitor until done
		monitor.MonitorStep(stateDir, step.ID, stepStart)

		duration := time.Since(stepStart)

		// Read exit code
		exitCode := 0
		exitFile := filepath.Join(stateDir, "status", step.ID+".exit")
		if data, err := os.ReadFile(exitFile); err == nil {
			if code, err := strconv.Atoi(string(data)); err == nil {
				exitCode = code
			}
		}

		if exitCode == 0 {
			display.LogSuccess(fmt.Sprintf("Step %s completed (%s)", step.ID, display.FormatDuration(duration)))
		} else {
			display.LogErr(fmt.Sprintf("Step %s failed with exit %d (%s)", step.ID, exitCode, display.FormatDuration(duration)))
		}

		// Check files
		if exitCode == 0 && len(step.CheckFiles) > 0 {
			for _, cf := range step.CheckFiles {
				checkPath := cf
				if !filepath.IsAbs(checkPath) {
					checkPath = filepath.Join(cfg.WorkDir, checkPath)
				}
				if _, err := os.Stat(checkPath); err != nil {
					display.LogErr(fmt.Sprintf("Expected file missing: %s", cf))
					exitCode = 1
				} else {
					display.LogSuccess(fmt.Sprintf("File exists: %s", cf))
				}
			}
		}

		results = append(results, stepResult{
			ID:       step.ID,
			Name:     step.Name,
			ExitCode: exitCode,
			Duration: duration,
		})

		if exitCode != 0 {
			display.LogErr("Stopping due to step failure")
			break
		}
	}

	// Summary
	fmt.Fprintln(os.Stderr)
	display.Header("Summary")

	failed := 0
	for _, r := range results {
		if r.Skipped {
			fmt.Fprintf(os.Stderr, "  %s %s %s\n",
				display.ColorDim("SKIP"), display.ColorDim(r.ID), display.ColorDim(r.Name))
			continue
		}
		if r.ExitCode == 0 {
			fmt.Fprintf(os.Stderr, "  %s %s %s (%s)\n",
				display.ColorGreen("PASS"), r.ID, r.Name, display.FormatDuration(r.Duration))
		} else {
			fmt.Fprintf(os.Stderr, "  %s %s %s (%s)\n",
				display.ColorRed("FAIL"), r.ID, r.Name, display.FormatDuration(r.Duration))
			failed++
		}
	}

	fmt.Fprintln(os.Stderr)
	if failed > 0 {
		display.LogErr(fmt.Sprintf("%d step(s) failed", failed))
		return 1
	}
	display.LogSuccess("All steps passed")
	return 0
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: idlebat <config.yaml> [flags]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --dry-run    Preview steps without executing")
	fmt.Fprintln(os.Stderr, "  --resume     Skip already-completed steps")
	fmt.Fprintln(os.Stderr, "  --version    Print version and exit")
	fmt.Fprintln(os.Stderr, "  --help       Show this help")
}
