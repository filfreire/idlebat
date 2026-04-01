//go:build darwin

package terminal

import (
	"fmt"
	"os"
	"os/exec"
)

type darwinSpawner struct{}

func NewSpawner() Spawner {
	return &darwinSpawner{}
}

func (d *darwinSpawner) SpawnStep(stepID, stepName, stateDir string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable path: %w", err)
	}

	script := fmt.Sprintf(`tell application "Terminal"
	activate
	do script "%s --internal-run %s %s"
end tell`, self, stateDir, stepID)

	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}
