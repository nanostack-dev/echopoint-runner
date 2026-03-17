package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
)

const (
	nextJobPath   = "/runner/jobs/next"
	heartbeatPath = "/runner/jobs/heartbeat"
)

var ErrNoJobAvailable = errors.New("no runner job available")

type Client struct {
	baseURL        string
	organizationID string
	runnerAPIKey   string
	httpClient     *http.Client
}

type Config struct {
	BaseURL        string
	OrganizationID string
	RunnerAPIKey   string
	RequestTimeout time.Duration
}

type ClaimNextRequest struct {
	RunnerID         string    `json:"runner_id"`
	BootID           uuid.UUID `json:"boot_id"`
	MaxParallelFlows int       `json:"max_parallel_flows"`
}

type ClaimedJob struct {
	JobID          uuid.UUID         `json:"job_id"`
	ExecutionID    uuid.UUID         `json:"execution_id"`
	FlowID         uuid.UUID         `json:"flow_id"`
	LeaseExpiresAt time.Time         `json:"lease_expires_at"`
	FlowDefinition json.RawMessage   `json:"flow_definition"`
	Environment    map[string]string `json:"environment"`
}

type CompleteJobRequest struct {
	RunnerID     string                  `json:"runner_id"`
	BootID       uuid.UUID               `json:"boot_id"`
	Status       string                  `json:"status"`
	StartedAt    time.Time               `json:"started_at"`
	CompletedAt  time.Time               `json:"completed_at"`
	DurationMs   int64                   `json:"duration_ms"`
	Result       *map[string]interface{} `json:"result,omitempty"`
	ErrorMessage *string                 `json:"error_message,omitempty"`
	ErrorCode    *string                 `json:"error_code,omitempty"`
}

type HeartbeatRequest struct {
	BootID uuid.UUID   `json:"boot_id"`
	JobIDs []uuid.UUID `json:"job_ids"`
}

type HeartbeatResult struct {
	JobID          uuid.UUID  `json:"job_id"`
	Status         string     `json:"status"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at"`
}

type APIErrorResponse struct {
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func NewClient(config Config) *Client {
	return &Client{
		baseURL:        strings.TrimRight(config.BaseURL, "/"),
		organizationID: config.OrganizationID,
		runnerAPIKey:   config.RunnerAPIKey,
		httpClient: &http.Client{
			Timeout: config.RequestTimeout,
		},
	}
}

func (c *Client) ClaimNext(ctx context.Context, request ClaimNextRequest) (*ClaimedJob, error) {
	statusCode, responseBody, requestErr := c.postJSON(ctx, nextJobPath, request)
	if requestErr != nil {
		return nil, requestErr
	}

	if statusCode == http.StatusNoContent {
		return nil, ErrNoJobAvailable
	}
	if statusCode != http.StatusOK {
		return nil, readAPIError(statusCode, responseBody)
	}

	var claimed ClaimedJob
	if decodeErr := json.Unmarshal(responseBody, &claimed); decodeErr != nil {
		return nil, fmt.Errorf("decode claimed runner job: %w", decodeErr)
	}

	return &claimed, nil
}

func (c *Client) Complete(ctx context.Context, jobID uuid.UUID, request CompleteJobRequest) error {
	path := fmt.Sprintf("/runner/jobs/%s/complete", jobID.String())
	statusCode, responseBody, requestErr := c.postJSON(ctx, path, request)
	if requestErr != nil {
		return requestErr
	}

	if statusCode != http.StatusNoContent {
		return readAPIError(statusCode, responseBody)
	}

	return nil
}

func (c *Client) Heartbeat(ctx context.Context, request HeartbeatRequest) ([]HeartbeatResult, error) {
	statusCode, responseBody, requestErr := c.postJSON(ctx, heartbeatPath, request)
	if requestErr != nil {
		return nil, requestErr
	}

	if statusCode != http.StatusOK {
		return nil, readAPIError(statusCode, responseBody)
	}

	var results []HeartbeatResult
	if decodeErr := json.Unmarshal(responseBody, &results); decodeErr != nil {
		return nil, fmt.Errorf("decode heartbeat response: %w", decodeErr)
	}

	return results, nil
}

func FlowExecutionResultToPayload(result *node.FlowExecutionResult) (map[string]interface{}, error) {
	if result == nil {
		return map[string]interface{}{}, nil
	}

	payload := map[string]interface{}{
		"execution_results": map[string]interface{}{},
		"final_outputs":     result.FinalOutputs,
		"success":           result.Success,
		"duration_ms":       result.DurationMS,
	}

	if result.ErrorCode != nil {
		payload["error_code"] = *result.ErrorCode
	}
	if result.ErrorMsg != nil {
		payload["error_message"] = *result.ErrorMsg
	}

	return payload, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload interface{}) (int, []byte, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.runnerAPIKey)
	req.Header.Set("X-Organization-Id", c.organizationID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return 0, nil, fmt.Errorf("read response body: %w", readErr)
	}

	return resp.StatusCode, responseBody, nil
}

func readAPIError(statusCode int, body []byte) error {
	var apiErr APIErrorResponse
	if unmarshalErr := json.Unmarshal(body, &apiErr); unmarshalErr == nil && len(apiErr.Errors) > 0 {
		first := apiErr.Errors[0]
		return fmt.Errorf("control plane error (%d): %s: %s", statusCode, first.Code, first.Message)
	}

	return fmt.Errorf("control plane error (%d): %s", statusCode, strings.TrimSpace(string(body)))
}
