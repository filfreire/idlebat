//go:build windows

package terminal

import (
	"fmt"
	"os"
	"os/exec"
)

type windowsSpawner struct{}

func NewSpawner() Spawner {
	return &windowsSpawner{}
}

func (w *windowsSpawner) SpawnStep(stepID, stepName, stateDir string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable path: %w", err)
	}

	internalCmd := fmt.Sprintf(`"%s" --internal-run "%s" "%s"`, self, stateDir, stepID)

	// Try Windows Terminal (wt.exe) first for tab support
	if wtPath, err := exec.LookPath("wt.exe"); err == nil {
		cmd := exec.Command(wtPath, "new-tab",
			"--title", fmt.Sprintf("idlebat: %s", stepName),
			"cmd", "/k", internalCmd)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}

	// Fallback: cmd /c start opens a new console window
	cmd := exec.Command("cmd", "/c", "start",
		fmt.Sprintf("idlebat: %s", stepName),
		"cmd", "/k", internalCmd)
	return cmd.Start()
}
