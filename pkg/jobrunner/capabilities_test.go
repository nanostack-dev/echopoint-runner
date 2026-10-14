package jobrunner_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/jobrunner"
	"github.com/stretchr/testify/assert"
)

// A caller that claims jobs itself, such as the CLI's ephemeral run, declares
// what the runner it embeds supports.
func TestCapabilitiesIncludeSecretOutputs(t *testing.T) {
	assert.Contains(t, jobrunner.Capabilities(), "secret_outputs")
}
