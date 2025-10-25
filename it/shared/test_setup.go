package shared

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type TestContext struct {
	WireMockURL string
}

var flowEngineContext *TestContext             //nolint:gochecknoglobals // Test context needed globally
var wiremockContainer testcontainers.Container //nolint:gochecknoglobals // Container needs to stay alive

func GetFlowEngineContext() *TestContext {
	return flowEngineContext
}

func LaunchTest(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() {
	ctx := context.Background()

	// Get absolute path to stubs directory
	wd, err := os.Getwd()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get working directory")
		os.Exit(1)
	}
	stubsPath := filepath.Join(wd, "wiremock", "stubs")

	// Start WireMock container with stubs
	req := testcontainers.ContainerRequest{
		Image:        "wiremock/wiremock:3.3.1",
		ExposedPorts: []string{"8080/tcp"},
		WaitingFor: wait.ForHTTP("/__admin/health").
			WithPort("8080").
			WithStartupTimeout(30 * time.Second),
		Cmd: []string{
			"--global-response-templating",
			"--verbose",
		},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.BindMount(stubsPath, "/home/wiremock/mappings"),
		},
	}

	wiremockContainer, containerErr := testcontainers.GenericContainer(
		ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		},
	)
	if containerErr != nil {
		log.Error().Err(containerErr).Msg("Failed to start wiremock container")
		os.Exit(1)
	}

	wiremockHost, hostErr := wiremockContainer.Host(ctx)
	if hostErr != nil {
		log.Error().Err(hostErr).Msg("Failed to get wiremock host")
		return // exit after defer
	}

	wiremockPort, portErr := wiremockContainer.MappedPort(ctx, "8080")
	if portErr != nil {
		log.Error().Err(portErr).Msg("Failed to get wiremock port")
		return // exit after defer
	}

	wiremockURL := fmt.Sprintf("http://%s:%s", wiremockHost, wiremockPort.Port())
	log.Info().Str("url", wiremockURL).Msg("WireMock started")
	//Wait to ensure WireMock is fully ready
	time.Sleep(2 * time.Second)

	flowEngineContext = &TestContext{
		WireMockURL: wiremockURL,
	}
}

func teardown() {
	if wiremockContainer != nil {
		if termErr := testcontainers.TerminateContainer(wiremockContainer); termErr != nil {
			log.Error().Err(termErr).Msg("Failed to terminate wiremock container")
		}
	}
}
