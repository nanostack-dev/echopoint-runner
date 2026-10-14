package jobrunner

import "github.com/nanostack-dev/echopoint-runner/internal/controlplane"

// Capabilities lists what this runner build supports. A caller that claims a
// job itself, such as the CLI's ephemeral run, declares them on its claim so
// the control plane can refuse a job this build cannot run safely.
func Capabilities() []string {
	return controlplane.Capabilities()
}
