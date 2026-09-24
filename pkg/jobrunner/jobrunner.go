// Package jobrunner runs one claimed Job using the same executor and reporter
// as the long-lived Self-hosted runner.
package jobrunner

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-runner/internal/config"
	"github.com/nanostack-dev/echopoint-runner/internal/controlplane"
	"github.com/nanostack-dev/echopoint-runner/internal/runtime"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type Job struct {
	JobID           uuid.UUID                   `json:"job_id"`
	ExecutionID     uuid.UUID                   `json:"execution_id"`
	FlowID          uuid.UUID                   `json:"flow_id"`
	LeaseExpiresAt  time.Time                   `json:"lease_expires_at"`
	FlowDefinition  json.RawMessage             `json:"flow_definition"`
	Inputs          map[string]any              `json:"inputs"`
	SecretInputKeys []string                    `json:"secret_input_keys,omitempty"`
	ReferencedFlows flow.ReferencedFlowRegistry `json:"referenced_flows,omitempty"`
}

type Config struct {
	BaseURL   string
	JobToken  string
	RunnerID  string
	BootID    uuid.UUID
	Timeout   time.Duration
	Heartbeat time.Duration
}

type Client struct {
	config Config
}

type Result struct {
	Status       string
	Execution    *spi.FlowExecutionResult
	ErrorMessage *string
}

const (
	defaultTimeout   = 45 * time.Second
	defaultHeartbeat = 10 * time.Second
)

func NewClient(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" || cfg.JobToken == "" || cfg.RunnerID == "" ||
		cfg.BootID == uuid.Nil {
		return nil, errors.New("one-shot Job runner requires a URL, token, and runner identity")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = defaultHeartbeat
	}
	return &Client{config: cfg}, nil
}

func (c *Client) Run(ctx context.Context, job Job) (Result, error) {
	if job.JobID == uuid.Nil {
		return Result{}, errors.New("one-shot Job runner requires a Job ID")
	}
	cfg := c.config
	outcome, err := runtime.RunOne(ctx, config.Config{
		BaseURL:           cfg.BaseURL,
		RunnerID:          cfg.RunnerID,
		MaxParallelFlows:  1,
		RequestTimeout:    cfg.Timeout,
		HeartbeatInterval: cfg.Heartbeat,
	}, cfg.JobToken, cfg.BootID, &controlplane.ClaimedJob{
		JobID:           job.JobID,
		ExecutionID:     job.ExecutionID,
		FlowID:          job.FlowID,
		LeaseExpiresAt:  job.LeaseExpiresAt,
		FlowDefinition:  job.FlowDefinition,
		Inputs:          job.Inputs,
		SecretInputKeys: job.SecretInputKeys,
		ReferencedFlows: job.ReferencedFlows,
	})
	return Result{Status: outcome.Status, Execution: outcome.Result, ErrorMessage: outcome.ErrorMessage}, err
}
