package runner_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/runner"
)

func TestRetry_RetriesUntilSuccess(t *testing.T) {
	calls := 0
	base := engine.NodeExecutor(func(node.ExecutionContext) (node.AnyExecutionResult, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("boom")
		}
		return &node.DelayExecutionResult{}, nil
	})

	if _, err := runner.Retry(5)(base)(node.ExecutionContext{}); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
}

func TestRetry_StopsOnCancelledContext(t *testing.T) {
	calls := 0
	base := engine.NodeExecutor(func(node.ExecutionContext) (node.AnyExecutionResult, error) {
		calls++
		return nil, errors.New("boom")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = runner.Retry(5)(base)(node.ExecutionContext{Ctx: ctx})
	if calls != 1 {
		t.Errorf("a cancelled context must stop retries after 1 attempt, got %d", calls)
	}
}

func TestTimeout_SetsDeadline(t *testing.T) {
	var hadDeadline bool
	base := engine.NodeExecutor(func(ec node.ExecutionContext) (node.AnyExecutionResult, error) {
		_, hadDeadline = ec.Context().Deadline()
		return &node.DelayExecutionResult{}, nil
	})

	if _, err := runner.Timeout(time.Second)(base)(node.ExecutionContext{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hadDeadline {
		t.Error("Timeout middleware should set a deadline on the execution context")
	}
}
