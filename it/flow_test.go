package it_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/internal/logger"
	"github.com/nanostack-dev/echopoint-flow-engine/it/shared"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/engine"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Enable debug logging with human-readable format for tests
	logger.SetDebugLogging()
}

// loadFlowFromJSON loads a flow definition from a JSON file.
func loadFlowFromJSON(t *testing.T, filename string) *flow.Flow {
	// Construct path to examples directory
	examplesDir := filepath.Join(".", "examples")
	flowPath := filepath.Join(examplesDir, filename)

	// Read the JSON file
	data, err := os.ReadFile(flowPath)
	require.NoError(t, err, "failed to read flow JSON file: %s", flowPath)

	// Parse the flow definition
	flowDef, err := flow.ParseFromJSON(data)
	require.NoError(t, err, "failed to parse flow definition")

	return flowDef
}

// TestDataContract_CreateUserFlow tests a realistic flow where:
// 1. Create a user via POST request (uses initial variables)
// 2. Extract user ID from response
// 3. Fetch the created user (uses extracted user ID from step 1)
// 4. Verify the data matches what was sent.
func Test_CreateUserFlow(t *testing.T) {
	ctx := shared.GetFlowEngineContext()
	require.NotNil(t, ctx, "test context should be initialized")

	// Load flow definition from JSON
	flowDef := loadFlowFromJSON(t, "create-user-flow-no-test.json")

	// Override initialInputs with actual WireMock URL
	flowDef.InitialInputs["apiUrl"] = ctx.WireMockURL

	// Create the engine
	flowEngine, err := engine.NewFlowEngine(*flowDef, nil)
	require.NoError(t, err, "engine creation should not fail")

	// Execute the flow
	result, err := flowEngine.Execute(flowDef.InitialInputs)

	// Verify execution succeeded
	require.NoError(t, err, "flow execution should not error")
	require.True(t, result.Success, "flow should execute successfully")
	require.Positive(t, result.DurationMS, "flow should track duration")

	// === VERIFY STEP 1: Create User ===
	createUserFrame := result.ExecutionResults["create-user"]
	require.NotNil(t, createUserFrame, "create-user frame should exist")

	// Verify inputs were properly assembled
	assert.Equal(t, ctx.WireMockURL, createUserFrame.GetInputs()["apiUrl"])
	assert.Equal(t, "Alice Smith", createUserFrame.GetInputs()["userName"])
	assert.Equal(t, "alice@example.com", createUserFrame.GetInputs()["userEmail"])

	// Verify request succeeded
	assert.Equal(t, 201, createUserFrame.GetOutputs()["statusCode"], "create should return 201")

	// Verify user ID was extracted
	userID := createUserFrame.GetOutputs()["userId"]
	require.NotNil(t, userID, "userId should be extracted")
	assert.Equal(t, "123", userID, "userId should match WireMock response")

	// Verify full user response was captured
	createdUserData := createUserFrame.GetOutputs()["createdUser"]
	require.NotNil(t, createdUserData, "createdUser should be extracted")

	// Parse the response to verify template substitution worked
	var createdUserMap map[string]interface{}
	createdUserBytes, _ := json.Marshal(createdUserData)
	err = json.Unmarshal(createdUserBytes, &createdUserMap)
	require.NoError(t, err, "failed to unmarshal created user response")

	userField := createdUserMap["user"].(map[string]interface{})
	assert.Equal(t, "Alice Smith", userField["name"], "name should match sent data")
	assert.Equal(t, "alice@example.com", userField["email"], "email should match sent data")

	// === VERIFY STEP 2: Verify User (uses data from step 1) ===
	verifyUserFrame := result.ExecutionResults["verify-user"]
	require.NotNil(t, verifyUserFrame, "verify-user frame should exist")

	// CRITICAL: Verify that data from step 1 was passed to step 2
	assert.Equal(
		t, "123", verifyUserFrame.GetInputs()["create-user.userId"],
		"verify step should receive userId from create step",
	)
	assert.Equal(
		t, ctx.WireMockURL, verifyUserFrame.GetInputs()["apiUrl"],
		"verify step should receive apiUrl from initial inputs",
	)

	assert.Equal(t, 200, verifyUserFrame.GetOutputs()["verifyStatus"], "verify should return 200")

	assert.Equal(t, 201, result.FinalOutputs["create-user.statusCode"])
	assert.Equal(t, "123", result.FinalOutputs["create-user.userId"])
	assert.Equal(t, 200, result.FinalOutputs["verify-user.verifyStatus"])
}
