package it_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/it/shared"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/engine"
	"github.com/nanostack-dev/echopoint-flow-engine/pkg/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadFlowFromJSON loads a flow definition from a JSON file
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
// 4. Verify the data matches what was sent
func Test_CreateUserFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := shared.GetFlowEngineContext()
	require.NotNil(t, ctx, "test context should be initialized")

	// Load flow definition from JSON
	flowDef := loadFlowFromJSON(t, "create-user-flow.json")

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
	require.Greater(t, result.DurationMS, int64(0), "flow should track duration")

	// === VERIFY STEP 1: Create User ===
	createUserFrame := result.Frames["create-user"]
	require.NotNil(t, createUserFrame, "create-user frame should exist")

	// Verify inputs were properly assembled
	assert.Equal(t, ctx.WireMockURL, createUserFrame.Inputs["apiUrl"])
	assert.Equal(t, "Alice Smith", createUserFrame.Inputs["userName"])
	assert.Equal(t, "alice@example.com", createUserFrame.Inputs["userEmail"])

	// Verify request succeeded
	assert.Equal(t, 201, createUserFrame.Outputs["statusCode"], "create should return 201")

	// Verify user ID was extracted
	userID := createUserFrame.Outputs["userId"]
	require.NotNil(t, userID, "userId should be extracted")
	assert.Equal(t, "123", userID, "userId should match WireMock response")

	// Verify full user response was captured
	createdUserData := createUserFrame.Outputs["createdUser"]
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
	verifyUserFrame := result.Frames["verify-user"]
	require.NotNil(t, verifyUserFrame, "verify-user frame should exist")

	// CRITICAL: Verify that data from step 1 was passed to step 2
	assert.Equal(
		t, "123", verifyUserFrame.Inputs["userId"],
		"verify step should receive userId from create step",
	)
	assert.Equal(
		t, ctx.WireMockURL, verifyUserFrame.Inputs["apiUrl"],
		"verify step should receive apiUrl from initial inputs",
	)

	// Verify the GET request succeeded
	assert.Equal(t, 200, verifyUserFrame.Outputs["verifyStatus"], "verify should return 200")

	// === VERIFY FINAL OUTPUTS ===
	assert.Equal(t, 201, result.FinalOutputs["create-user.statusCode"])
	assert.Equal(t, "123", result.FinalOutputs["create-user.userId"])
	assert.Equal(t, 200, result.FinalOutputs["verify-user.verifyStatus"])

	t.Logf("✓ Flow executed successfully")
	t.Logf("✓ Created user with ID: %v", userID)
	t.Logf("✓ Data properly passed from create-user to verify-user step")
	t.Logf("✓ Total execution time: %d ms", result.DurationMS)
}

// TestDataContract_CreateUserWithInitialInputs verifies that initial inputs
// are properly passed to the first node and available throughout the flow
func TestDataContract_CreateUserWithInitialInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := shared.GetFlowEngineContext()
	require.NotNil(t, ctx, "test context should be initialized")

	// Load flow definition from JSON
	flowDef := loadFlowFromJSON(t, "simple-create-user-flow.json")

	// Override initialInputs with actual WireMock URL
	flowDef.InitialInputs["apiUrl"] = ctx.WireMockURL

	// Create the engine
	flowEngine, err := engine.NewFlowEngine(*flowDef, nil)
	require.NoError(t, err)

	// Execute the flow
	result, err := flowEngine.Execute(flowDef.InitialInputs)

	require.NoError(t, err, "flow should execute successfully")
	require.True(t, result.Success)

	// Verify initial inputs were passed to the node
	frame := result.Frames["create-user"]
	assert.Equal(t, ctx.WireMockURL, frame.Inputs["initial.apiUrl"])
	assert.Equal(t, "Bob Johnson", frame.Inputs["initial.userName"])
	assert.Equal(t, "bob@example.com", frame.Inputs["initial.userEmail"])

	// Verify HTTP request was made (status 201)
	assert.Equal(t, 201, frame.Outputs["statusCode"])
	assert.Equal(t, "123", frame.Outputs["userId"])

	t.Logf("✓ Initial inputs correctly passed to HTTP request")
	t.Logf("✓ HTTP request sent with variable substitution: POST %s/users", ctx.WireMockURL)
	t.Logf("✓ Response parsed and data extracted")
}
