package terminal

// Spawner spawns a step in a new terminal window/tab.
type Spawner interface {
	SpawnStep(stepID, stepName, stateDir string) error
}
