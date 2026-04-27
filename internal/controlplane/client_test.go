package controlplane_test

import (
	"encoding/json"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimedJob_UnmarshalReferencedFlows(t *testing.T) {
	payload := []byte(`{
		"job_id": "0195d8a3-d120-7a4f-9bc1-97ddf4b72eb7",
		"execution_id": "0195d8a3-d121-7a4f-9bc1-97ddf4b72eb7",
		"flow_id": "0195d8a3-d122-7a4f-9bc1-97ddf4b72eb7",
		"lease_expires_at": "2026-04-27T12:00:00Z",
		"flow_definition": {"name": "Parent", "version": "1.0", "nodes": [], "edges": []},
		"environment": {"BASE_URL": "https://api.example.com"},
		"referenced_flows": {
			"flow-charge": {
				"flow_definition": {"name": "Child", "version": "1.0", "nodes": [], "edges": []},
				"environment": {"TOKEN": "secret"}
			}
		}
	}`)

	var claimedJob controlplane.ClaimedJob
	err := json.Unmarshal(payload, &claimedJob)
	require.NoError(t, err)
	require.Contains(t, claimedJob.ReferencedFlows, "flow-charge")
	assert.JSONEq(
		t,
		`{"name":"Child","version":"1.0","nodes":[],"edges":[]}`,
		string(claimedJob.ReferencedFlows["flow-charge"].FlowDefinition),
	)
	assert.Equal(t, "secret", claimedJob.ReferencedFlows["flow-charge"].Environment["TOKEN"])
}
