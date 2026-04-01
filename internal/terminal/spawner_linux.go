//go:build linux

package terminal

import (
	"fmt"
	"os"
	"os/exec"
)

type linuxSpawner struct{}

func NewSpawner() Spawner {
	return &linuxSpawner{}
}

func (l *linuxSpawner) SpawnStep(stepID, stepName, stateDir string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable path: %w", err)
	}

	internalCmd := fmt.Sprintf(`%s --internal-run %s %s`, self, stateDir, stepID)

	// Try common terminal emulators in order
	terminals := []struct {
		bin  string
		args []string
	}{
		{"gnome-terminal", []string{"--title", fmt.Sprintf("idlebat: %s", stepName), "--", "bash", "-c", internalCmd + "; exec bash"}},
		{"konsole", []string{"--title", fmt.Sprintf("idlebat: %s", stepName), "-e", "bash", "-c", internalCmd + "; exec bash"}},
		{"xfce4-terminal", []string{"--title", fmt.Sprintf("idlebat: %s", stepName), "-e", "bash -c '" + internalCmd + "; exec bash'"}},
		{"xterm", []string{"-title", fmt.Sprintf("idlebat: %s", stepName), "-e", internalCmd}},
		{"x-terminal-emulator", []string{"-e", internalCmd}},
	}

	for _, t := range terminals {
		if bin, err := exec.LookPath(t.bin); err == nil {
			cmd := exec.Command(bin, t.args...)
			if err := cmd.Start(); err == nil {
				return nil
			}
		}
	}

	// Fallback: run in foreground
	fmt.Fprintf(os.Stderr, "No terminal emulator found -- running step in foreground\n")
	cmd := exec.Command("bash", "-c", internalCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
