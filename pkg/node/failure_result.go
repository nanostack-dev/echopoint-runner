package node

import (
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// failureDetails is chosen by each node's error policy, not inferred here.
type failureDetails struct {
	Kind    spi.Kind
	Code    string
	Message string
}

func failedNodeBase(n BaseNode, inputs map[string]any, err error, details failureDetails) spi.BaseExecutionResult {
	return spi.BaseExecutionResult{
		NodeID:      n.ID,
		DisplayName: n.DisplayName,
		NodeType:    details.Kind,
		Inputs:      inputs,
		Outputs:     nil,
		Error:       err,
		ErrorMsg:    &details.Message,
		ErrorCode:   &details.Code,
		ExecutedAt:  time.Now(),
	}
}
