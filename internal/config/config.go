package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

const (
	defaultBaseURL             = "https://apidev.echopoint.dev"
	defaultMaxParallelFlows    = 1
	defaultHeartbeatInterval   = 10 * time.Second
	defaultRequestTimeout      = 45 * time.Second
	defaultIdleBackoff         = 1 * time.Second
	defaultErrorBackoff        = 3 * time.Second
	defaultShutdownGracePeriod = 45 * time.Second
	defaultLogLevel            = "info"
	defaultLogFormat           = "json"
	defaultDotEnvPath          = ".env"
)

type Config struct {
	BaseURL             string
	OrganizationID      string
	RunnerAPIKey        string
	RunnerID            string
	MaxParallelFlows    int
	HeartbeatInterval   time.Duration
	RequestTimeout      time.Duration
	IdleBackoff         time.Duration
	ErrorBackoff        time.Duration
	ShutdownGracePeriod time.Duration
	LogLevel            zerolog.Level
	LogFormat           string
	FlowAPIKey          string
}

func Load() (Config, error) {
	if err := loadDotEnv(defaultDotEnvPath); err != nil {
		return Config{}, err
	}

	logLevel, err := parseLogLevel(getEnvOrDefault("ECHOPOINT_LOG_LEVEL", defaultLogLevel))
	if err != nil {
		return Config{}, err
	}

	maxParallelFlows, err := getIntEnv("ECHOPOINT_MAX_PARALLEL_FLOWS", defaultMaxParallelFlows)
	if err != nil {
		return Config{}, err
	}

	heartbeatInterval, err := getDurationEnv("ECHOPOINT_HEARTBEAT_INTERVAL", defaultHeartbeatInterval)
	if err != nil {
		return Config{}, err
	}

	requestTimeout, err := getDurationEnv("ECHOPOINT_REQUEST_TIMEOUT", defaultRequestTimeout)
	if err != nil {
		return Config{}, err
	}

	idleBackoff, err := getDurationEnv("ECHOPOINT_IDLE_BACKOFF", defaultIdleBackoff)
	if err != nil {
		return Config{}, err
	}

	errorBackoff, err := getDurationEnv("ECHOPOINT_ERROR_BACKOFF", defaultErrorBackoff)
	if err != nil {
		return Config{}, err
	}

	shutdownGracePeriod, err := getDurationEnv(
		"ECHOPOINT_SHUTDOWN_GRACE_PERIOD",
		defaultShutdownGracePeriod,
	)
	if err != nil {
		return Config{}, err
	}

	config := Config{
		BaseURL:             strings.TrimRight(getEnvOrDefault("ECHOPOINT_BASE_URL", defaultBaseURL), "/"),
		OrganizationID:      strings.TrimSpace(os.Getenv("ECHOPOINT_ORGANIZATION_ID")),
		RunnerAPIKey:        strings.TrimSpace(os.Getenv("ECHOPOINT_RUNNER_API_KEY")),
		RunnerID:            strings.TrimSpace(os.Getenv("ECHOPOINT_RUNNER_ID")),
		MaxParallelFlows:    maxParallelFlows,
		HeartbeatInterval:   heartbeatInterval,
		RequestTimeout:      requestTimeout,
		IdleBackoff:         idleBackoff,
		ErrorBackoff:        errorBackoff,
		ShutdownGracePeriod: shutdownGracePeriod,
		LogLevel:            logLevel,
		LogFormat:           strings.TrimSpace(getEnvOrDefault("ECHOPOINT_LOG_FORMAT", defaultLogFormat)),
		FlowAPIKey:          strings.TrimSpace(os.Getenv("ECHOPOINT_FLOW_API_KEY")),
	}

	if config.OrganizationID == "" {
		return Config{}, errors.New("ECHOPOINT_ORGANIZATION_ID is required")
	}
	if config.RunnerAPIKey == "" {
		return Config{}, errors.New("ECHOPOINT_RUNNER_API_KEY is required")
	}
	if config.RunnerID == "" {
		return Config{}, errors.New("ECHOPOINT_RUNNER_ID is required")
	}
	if config.MaxParallelFlows < 1 {
		return Config{}, errors.New("ECHOPOINT_MAX_PARALLEL_FLOWS must be at least 1")
	}
	if config.HeartbeatInterval <= 0 {
		return Config{}, errors.New("ECHOPOINT_HEARTBEAT_INTERVAL must be positive")
	}
	if config.RequestTimeout <= 0 {
		return Config{}, errors.New("ECHOPOINT_REQUEST_TIMEOUT must be positive")
	}
	if config.IdleBackoff <= 0 {
		return Config{}, errors.New("ECHOPOINT_IDLE_BACKOFF must be positive")
	}
	if config.ErrorBackoff <= 0 {
		return Config{}, errors.New("ECHOPOINT_ERROR_BACKOFF must be positive")
	}
	if config.ShutdownGracePeriod <= 0 {
		return Config{}, errors.New("ECHOPOINT_SHUTDOWN_GRACE_PERIOD must be positive")
	}

	return config, nil
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("open dotenv file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("invalid dotenv line: %q", line)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)

		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		if setErr := os.Setenv(key, value); setErr != nil {
			return fmt.Errorf("set env %s: %w", key, setErr)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return fmt.Errorf("scan dotenv file: %w", scanErr)
	}

	return nil
}

func getEnvOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func getIntEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return parsed, nil
}

func getDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return parsed, nil
}

func parseLogLevel(value string) (zerolog.Level, error) {
	level, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(value)))
	if err != nil {
		return zerolog.InfoLevel, fmt.Errorf("parse ECHOPOINT_LOG_LEVEL: %w", err)
	}

	return level, nil
}
