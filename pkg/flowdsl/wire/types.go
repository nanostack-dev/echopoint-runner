// Package wire is the JSON shape of a flow the API stores.
// It is the subset of the OpenAPI models the flow file compiler reads and writes.
package wire

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	AssertFlowNodeTypeAssert AssertFlowNodeType = "assert"
)

const (
	BranchFlowNodeTypeBranch BranchFlowNodeType = "branch"
)

const (
	DelayFlowNodeTypeDelay DelayFlowNodeType = "delay"
)

const (
	ExtractorTypeBody       ExtractorType = "body"
	ExtractorTypeHeader     ExtractorType = "header"
	ExtractorTypeJsonPath   ExtractorType = "json_path"
	ExtractorTypeQueryParam ExtractorType = "query_param"
	ExtractorTypeStatusCode ExtractorType = "status_code"
	ExtractorTypeXmlPath    ExtractorType = "xml_path"
)

const (
	Failure FlowEdgeType = "failure"
	Success FlowEdgeType = "success"
)

const (
	Always    FlowNodeRunWhen = "always"
	OnSuccess FlowNodeRunWhen = "on_success"
)

const (
	FlowRoleModule     FlowRole = "module"
	FlowRoleStandalone FlowRole = "standalone"
)

const (
	DELETE  HttpMethod = "DELETE"
	GET     HttpMethod = "GET"
	HEAD    HttpMethod = "HEAD"
	OPTIONS HttpMethod = "OPTIONS"
	PATCH   HttpMethod = "PATCH"
	POST    HttpMethod = "POST"
	PUT     HttpMethod = "PUT"
)

const (
	LoopFlowNodeTypeLoop LoopFlowNodeType = "loop"
)

const (
	ModuleFlowNodeTypeModule ModuleFlowNodeType = "module"
)

const (
	OperatorTypeBetween            OperatorType = "between"
	OperatorTypeContains           OperatorType = "contains"
	OperatorTypeEmpty              OperatorType = "empty"
	OperatorTypeEndsWith           OperatorType = "ends_with"
	OperatorTypeEquals             OperatorType = "equals"
	OperatorTypeGreaterThan        OperatorType = "greater_than"
	OperatorTypeGreaterThanOrEqual OperatorType = "greater_than_or_equal"
	OperatorTypeLessThan           OperatorType = "less_than"
	OperatorTypeLessThanOrEqual    OperatorType = "less_than_or_equal"
	OperatorTypeNotContains        OperatorType = "not_contains"
	OperatorTypeNotEmpty           OperatorType = "not_empty"
	OperatorTypeNotEquals          OperatorType = "not_equals"
	OperatorTypeRegex              OperatorType = "regex"
	OperatorTypeStartsWith         OperatorType = "starts_with"
)

const (
	PollFlowNodeTypePoll PollFlowNodeType = "poll"
)

const (
	RequestFlowNodeTypeRequest RequestFlowNodeType = "request"
)

const (
	SetVariableFlowNodeTypeSetVariable SetVariableFlowNodeType = "set_variable"
)

const (
	SseFlowNodeTypeSse SseFlowNodeType = "sse"
)

const (
	WebhookWaitFlowNodeTypeWebhookWait WebhookWaitFlowNodeType = "webhook_wait"
)

func (e HttpMethod) Valid() bool {
	switch e {
	case DELETE:
		return true
	case GET:
		return true
	case HEAD:
		return true
	case OPTIONS:
		return true
	case PATCH:
		return true
	case POST:
		return true
	case PUT:
		return true
	default:
		return false
	}
}

// CompositeAssertion defines model for CompositeAssertion.
type CompositeAssertion struct {
	// ExtractorData Configuration for the extractor
	//
	// Examples: {"path":"$.user.name"}
	ExtractorData map[string]interface{} `json:"extractor_data"`

	// ExtractorType Type of extractor to use for data extraction.
	// `body` gives the parsed body. When the body is a JSON object or array, the text operators
	// (equals, not_equals, contains, not_contains, starts_with, ends_with, regex) compare the body text as received.
	// `query_param` reads a query parameter of the request a `webhook_wait` node received;
	// an HTTP response has none.
	ExtractorType ExtractorType `json:"extractor_type"`

	// OperatorData Configuration for the operator
	//
	// Examples: {"max":299,"min":200}
	OperatorData map[string]interface{} `json:"operator_data"`

	// OperatorType Operator to apply in assertions
	OperatorType OperatorType `json:"operator_type"`
}

// CreateFlowRequest defines model for CreateFlowRequest.
type CreateFlowRequest struct {
	// AutoLayout Discard any node positions sent with the flow and clear every edge's pinned borders. The editor lays the flow out from the size each node renders at the next time it is opened, and saves those positions. Omitting node_positions has the same effect on positions.
	AutoLayout *bool `json:"auto_layout,omitempty"`

	// Description Optional description of the flow.
	//
	// Examples: Test user endpoints with branching logic
	Description    *string        `json:"description,omitempty"`
	FlowDefinition FlowDefinition `json:"flow_definition"`

	// FolderId Folder to create the flow in. Omit or send null to leave it uncategorized. A folder unknown to the organization yields 400 INVALID_FOLDER_ID.
	FolderId *uuid.UUID `json:"folder_id,omitempty"`

	// Metadata Frontend-specific metadata including UI layout information and execution defaults.
	Metadata *FlowMetadata `json:"metadata,omitempty"`

	// Name Human-readable name for the flow.
	//
	// Examples: User API Test Flow
	Name string `json:"name"`

	// Role How the flow may be run. Omitted defaults to `standalone`.
	Role *FlowRole `json:"role,omitempty"`

	// Tags Optional flat tags for the flow. Lowercased, normalized, deduplicated, and sorted on write. Omitted defaults to an empty list.
	//
	// Examples: ["production","monitoring"]
	Tags *[]string `json:"tags,omitempty"`

	// Version Version identifier for the flow.
	//
	// Examples: 1.0
	Version *string `json:"version,omitempty"`
}

// Flow defines model for Flow.
type Flow struct {
	// CreatedAt Timestamp when the flow was created.
	CreatedAt time.Time `json:"created_at"`

	// Description Optional description of what the flow does.
	//
	// Examples: Test user endpoints with branching logic
	Description    *string        `json:"description,omitempty"`
	FlowDefinition FlowDefinition `json:"flow_definition"`

	// FolderId The flow's folder, or null when it is uncategorized.
	FolderId *uuid.UUID `json:"folder_id,omitempty"`

	// Id Server-generated unique identifier for the flow.
	//
	// Examples: 550e8400-e29b-41d4-a716-446655440000
	Id uuid.UUID `json:"id"`

	// Metadata Frontend-specific metadata including UI layout information and execution defaults.
	Metadata FlowMetadata `json:"metadata"`

	// Name Human-readable name for the flow.
	//
	// Examples: User API Test Flow
	Name string `json:"name"`

	// OrganizationId Organization ID that owns this flow.
	//
	// Examples: org_123
	OrganizationId string `json:"organization_id"`

	// Role How a flow may be run. A `standalone` flow is a test of its own. A `module` flow is a
	// reusable building block that other flows call through a module node: you can still
	// launch it directly to test it, but schedules skip it when their tags match it unless
	// the schedule sets `ignore_modules` to false.
	Role FlowRole `json:"role"`

	// Tags Flat canonical tags associated with the flow (lowercase, normalized, deduplicated, sorted).
	//
	// Examples: ["production","monitoring"]
	Tags []string `json:"tags"`

	// UpdatedAt Timestamp when the flow was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// Version Version identifier for the flow.
	//
	// Examples: 1.0
	Version string `json:"version"`

	// WorkspaceId Reserved workspace ownership. Absent or null in this stage; resources remain organization-scoped.
	WorkspaceId *string `json:"workspace_id,omitempty"`
}

// FlowDefinition defines model for FlowDefinition.
type FlowDefinition struct {
	// Description Description of what the flow does
	//
	// Examples: Test user endpoints with branching
	Description *string `json:"description,omitempty"`

	// Edges Connections between nodes
	Edges []FlowEdge `json:"edges"`

	// Name Name of the flow
	//
	// Examples: User API Test
	Name string `json:"name"`

	// Nodes List of nodes in the flow
	Nodes []FlowNode `json:"nodes"`

	// Version Flow definition version
	//
	// Examples: 1.0
	Version string `json:"version"`
}

// FlowEdge defines model for FlowEdge.
type FlowEdge struct {
	// Id Unique identifier for the edge
	//
	// Examples: e-success
	Id string `json:"id"`

	// Source ID of the source node. It must name a node of the same definition; a create or update whose edge names an unknown node returns 400 UNKNOWN_EDGE_NODE.
	//
	// Examples: req-1
	Source string `json:"source"`

	// SourceHandle Border of the source node the edge is pinned to: top, right, bottom, or left. Absent means the endpoint floats and meets the border that faces the target node. An auto-layout update clears this field.
	//
	// Examples: right
	SourceHandle *string `json:"source_handle,omitempty"`

	// Target ID of the target node. It must name a node of the same definition; a create or update whose edge names an unknown node returns 400 UNKNOWN_EDGE_NODE.
	//
	// Examples: req-success
	Target string `json:"target"`

	// TargetHandle Border of the target node the edge is pinned to: top, right, bottom, or left. Absent means the endpoint floats and meets the border that faces the source node. An auto-layout update clears this field.
	//
	// Examples: left
	TargetHandle *string `json:"target_handle,omitempty"`

	// Type Condition for following this edge
	//
	// Examples: success
	Type FlowEdgeType `json:"type"`
}

// FlowEdgeType Condition for following this edge
//
// Examples: success
type FlowEdgeType string

// FlowMetadata Frontend-specific metadata including UI layout information and execution defaults.
type FlowMetadata struct {
	// NodePositions Map of node IDs to the top-left x,y of each node in the editor. A node missing from the map is placed by the editor beside the nodes it connects to, from the size it renders at, and never moves a node that has a position. Agents creating or extending a flow can leave positions out.
	NodePositions *map[string]struct {
		X *float32 `json:"x,omitempty"`
		Y *float32 `json:"y,omitempty"`
	} `json:"node_positions,omitempty"`

	// TargetEnvironmentKey Optional default organization environment key to use when launching this flow.
	//
	// Examples: staging
	TargetEnvironmentKey *string                `json:"target_environment_key,omitempty"`
	AdditionalProperties map[string]interface{} `json:"-"`
}

// FlowNode A node in a flow definition, discriminated by `type`. Every variant here is a
// kind the runner registers and can execute; the contract and
// `../echopoint-runner/pkg/spi.Kind` must stay in step, because an unlisted
// type fails discriminator binding and the flow is rejected with 400.
type FlowNode struct {
	union json.RawMessage
}

// FlowNodeRunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
type FlowNodeRunWhen string

// FlowRole How a flow may be run. A `standalone` flow is a test of its own. A `module` flow is a
// reusable building block that other flows call through a module node: you can still
// launch it directly to test it, but schedules skip it when their tags match it unless
// the schedule sets `ignore_modules` to false.
type FlowRole string

// HttpMethod HTTP method
type HttpMethod string

// InlineFlowDefinition A nested flow definition executed by a loop or poll node. It has the same shape
// as a flow's own definition, but is stored inline on the node rather than
// referenced by id the way a module node references a child flow.
type InlineFlowDefinition struct {
	Edges *[]FlowEdge `json:"edges,omitempty"`
	Nodes []FlowNode  `json:"nodes"`
}

// ExtractorType Type of extractor to use for data extraction.
// `body` gives the parsed body. When the body is a JSON object or array, the text operators
// (equals, not_equals, contains, not_contains, starts_with, ends_with, regex) compare the body text as received.
// `query_param` reads a query parameter of the request a `webhook_wait` node received;
// an HTTP response has none.
type ExtractorType string

// OperatorType Operator to apply in assertions
type OperatorType string

// Output defines model for Output.
type Output struct {
	// Extractor Extractor configuration that defines how to extract data from the response
	Extractor struct {
		// HeaderName Header name to extract (used by header extractor)
		//
		// Examples: x-custom-id
		HeaderName *string `json:"header_name,omitempty"`

		// Path Path for extraction (used by json_path, xml_path, header extractors)
		//
		// Examples: $.user.id
		Path *string `json:"path,omitempty"`

		// Type Type of extractor to use for data extraction.
		// `body` gives the parsed body. When the body is a JSON object or array, the text operators
		// (equals, not_equals, contains, not_contains, starts_with, ends_with, regex) compare the body text as received.
		// `query_param` reads a query parameter of the request a `webhook_wait` node received;
		// an HTTP response has none.
		Type ExtractorType `json:"type"`
	} `json:"extractor"`

	// Name Name of the output for reference by downstream nodes
	//
	// Examples: userId
	Name string `json:"name"`

	// Secret Marks the extracted value as secret. From the moment the node extracts it, the value is replaced by `***` in every execution result, progress event and error, wherever it appears, including module outputs and downstream node inputs and requests that carry it. Downstream nodes still receive the real value. Use it for credentials and tokens; a short or common value masks every occurrence of that text.
	Secret *bool `json:"secret,omitempty"`
}

// AssertFlowNode defines model for AssertFlowNode.
type AssertFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`

	// Data Runs the node's `assertions` against a value without issuing a request. Omit
	// `target` to assert over the flow's initial inputs; set it to a template
	// reference (e.g. `{{{create-user.payload}}}`) to assert over an upstream node's
	// output.
	Data AssertNodeData `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen   `json:"run_when,omitempty"`
	Type    AssertFlowNodeType `json:"type"`
}

// AssertFlowNodeType defines model for AssertFlowNode.Type.
type AssertFlowNodeType string

// AssertNodeData Runs the node's `assertions` against a value without issuing a request. Omit
// `target` to assert over the flow's initial inputs; set it to a template
// reference (e.g. `{{{create-user.payload}}}`) to assert over an upstream node's
// output.
type AssertNodeData struct {
	// Target Value the assertions run against. Templated; omit to use the flow's initial inputs.
	//
	// Examples: {{{create-user.payload}}}
	Target interface{} `json:"target,omitempty"`
}

// BranchCase defines model for BranchCase.
type BranchCase struct {
	// Target Node id to route to when this case matches.
	//
	// Examples: active-path
	Target string             `json:"target"`
	When   CompositeAssertion `json:"when"`
}

// BranchFlowNode defines model for BranchFlowNode.
type BranchFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`

	// Data Routes execution to one of several successor nodes. Cases are evaluated in
	// order and the first match wins; `default` is taken when none match. Every
	// target must be a real successor of this node — the flow is rejected at launch
	// when an edge to it is missing.
	Data BranchNodeData `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen   `json:"run_when,omitempty"`
	Type    BranchFlowNodeType `json:"type"`
}

// BranchFlowNodeType defines model for BranchFlowNode.Type.
type BranchFlowNodeType string

// BranchNodeData Routes execution to one of several successor nodes. Cases are evaluated in
// order and the first match wins; `default` is taken when none match. Every
// target must be a real successor of this node — the flow is rejected at launch
// when an edge to it is missing.
type BranchNodeData struct {
	// Cases Ordered conditions; the first matching case decides the route.
	Cases []BranchCase `json:"cases"`

	// Default Node id taken when no case matches. Without it, an unmatched branch ends that path.
	//
	// Examples: fallback
	Default *string `json:"default,omitempty"`

	// Target Value the case conditions are evaluated against. Templated; omit to use the flow's initial inputs.
	//
	// Examples: {{probe.status}}
	Target interface{} `json:"target,omitempty"`
}

// DelayFlowNode defines model for DelayFlowNode.
type DelayFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`
	Data       DelayNodeData         `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen  `json:"run_when,omitempty"`
	Type    DelayFlowNodeType `json:"type"`
}

// DelayFlowNodeType defines model for DelayFlowNode.Type.
type DelayFlowNodeType string

// DelayNodeData defines model for DelayNodeData.
type DelayNodeData struct {
	// Duration Delay duration in milliseconds
	//
	// Examples: 1000
	Duration int `json:"duration"`
}

// LoopFlowNode defines model for LoopFlowNode.
type LoopFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`

	// Data Runs an inline sub-flow once per item. The current item and index are injected
	// into the body's inputs under `item_var`/`index_var`.
	Data LoopNodeData `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen `json:"run_when,omitempty"`
	Type    LoopFlowNodeType `json:"type"`
}

// LoopFlowNodeType defines model for LoopFlowNode.Type.
type LoopFlowNodeType string

// LoopNodeData Runs an inline sub-flow once per item. The current item and index are injected
// into the body's inputs under `item_var`/`index_var`.
type LoopNodeData struct {
	// Body A nested flow definition executed by a loop or poll node. It has the same shape
	// as a flow's own definition, but is stored inline on the node rather than
	// referenced by id the way a module node references a child flow.
	Body InlineFlowDefinition `json:"body"`

	// ContinueOnError Keep looping when an iteration fails, recording the error in that iteration's result.
	ContinueOnError *bool `json:"continue_on_error,omitempty"`

	// IndexVar Input key the current zero-based index is injected under.
	IndexVar *string `json:"index_var,omitempty"`

	// ItemVar Input key the current item is injected under.
	ItemVar *string `json:"item_var,omitempty"`

	// Items Template resolving to an array, or a literal array.
	//
	// Examples: ["a","b","c"]
	Items interface{} `json:"items"`

	// MaxIterations Safety cap on iterations. Unbounded when omitted or zero.
	//
	// Examples: 100
	MaxIterations *int `json:"max_iterations,omitempty"`
}

// ModuleFlowNode defines model for ModuleFlowNode.
type ModuleFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`
	Data       ModuleNodeData        `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen   `json:"run_when,omitempty"`
	Type    ModuleFlowNodeType `json:"type"`
}

// ModuleFlowNodeType defines model for ModuleFlowNode.Type.
type ModuleFlowNodeType string

// ModuleNodeData defines model for ModuleNodeData.
type ModuleNodeData struct {
	// FlowId Referenced child flow executed by this module node.
	FlowId uuid.UUID `json:"flow_id"`

	// InputBindings Inputs bound from parent flow variables and upstream outputs.
	InputBindings *map[string]interface{} `json:"input_bindings,omitempty"`

	// OutputBindings Parent-visible outputs mapped from child final output keys.
	OutputBindings *map[string]string `json:"output_bindings,omitempty"`
}

// PollFlowNode defines model for PollFlowNode.
type PollFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`

	// Data Re-runs an inline sub-flow until the node's `assertions` pass against its final
	// outputs, or the attempt/time budget runs out. Body outputs are addressed by the
	// flat `nodeId.outputName` key (e.g. `$["probe.status"]`).
	Data PollNodeData `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen `json:"run_when,omitempty"`
	Type    PollFlowNodeType `json:"type"`
}

// PollFlowNodeType defines model for PollFlowNode.Type.
type PollFlowNodeType string

// PollNodeData Re-runs an inline sub-flow until the node's `assertions` pass against its final
// outputs, or the attempt/time budget runs out. Body outputs are addressed by the
// flat `nodeId.outputName` key (e.g. `$["probe.status"]`).
type PollNodeData struct {
	// Body A nested flow definition executed by a loop or poll node. It has the same shape
	// as a flow's own definition, but is stored inline on the node rather than
	// referenced by id the way a module node references a child flow.
	Body InlineFlowDefinition `json:"body"`

	// IntervalMs Wait between attempts, in milliseconds.
	IntervalMs *int `json:"interval_ms,omitempty"`

	// MaxAttempts Maximum body executions.
	MaxAttempts *int `json:"max_attempts,omitempty"`

	// TimeoutMs Overall wall-clock budget for the whole poll. Unbounded when omitted or zero.
	//
	// Examples: 60000
	TimeoutMs *int `json:"timeout_ms,omitempty"`
}

// RequestFlowNode defines model for RequestFlowNode.
type RequestFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`
	Data       RequestNodeData       `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen    `json:"run_when,omitempty"`
	Type    RequestFlowNodeType `json:"type"`
}

// RequestFlowNodeType defines model for RequestFlowNode.Type.
type RequestFlowNodeType string

// RequestNodeData defines model for RequestNodeData.
type RequestNodeData struct {
	// Body Request body (can be any JSON structure). Supports variable substitution.
	// Any string values within the body can contain template variables.
	// A string body is sent as-is; any other value is serialized to JSON.
	//
	//
	// Examples: {"email":"{{userEmail}}","name":"{{userName}}"}
	Body interface{} `json:"body,omitempty"`

	// Headers HTTP headers. Supports the same variable substitution as URL.
	// Headers can reference both initial inputs and previous node outputs.
	//
	// Content-Type of a request with a body:
	// - A Content-Type set here wins; the header name is matched case-insensitively.
	// - Without one, a JSON body is sent as application/json and a non-JSON string body is sent with no Content-Type.
	// - An empty Content-Type value sends no Content-Type header.
	//
	//
	// Examples: {"Authorization":"Bearer {{auth.token}}","Content-Type":"application/json"}
	Headers *map[string]string `json:"headers,omitempty"`

	// Method HTTP method
	//
	// Examples: POST
	Method HttpMethod `json:"method"`

	// QueryParams Query parameters. Supports variable substitution.
	// All keys and values can contain template variables.
	QueryParams *map[string]interface{} `json:"query_params,omitempty"`

	// Timeout Request timeout in milliseconds
	//
	// Examples: 30000
	Timeout *int `json:"timeout,omitempty"`

	// Url Full URL for the request. Supports variable substitution using template syntax.
	// - Initial variables: {{variableName}} (e.g., {{apiUrl}})
	// - Node outputs: {{nodeId.outputKey}} (e.g., {{create-user.userId}})
	//
	//
	// Examples: {{apiUrl}}/users/{{create-user.userId}}
	Url string `json:"url"`
}

// SetVariableFlowNode defines model for SetVariableFlowNode.
type SetVariableFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`

	// Data Declares flow variables. Values are templated, so a set_variable node is how a
	// flow names a computed value once and reuses it downstream as `{{nodeId.name}}`.
	Data SetVariableNodeData `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen        `json:"run_when,omitempty"`
	Type    SetVariableFlowNodeType `json:"type"`
}

// SetVariableFlowNodeType defines model for SetVariableFlowNode.Type.
type SetVariableFlowNodeType string

// SetVariableNodeData Declares flow variables. Values are templated, so a set_variable node is how a
// flow names a computed value once and reuses it downstream as `{{nodeId.name}}`.
type SetVariableNodeData struct {
	// Variables Variable name to value. String values support variable substitution.
	//
	// Examples: {"retries":3,"tenant":"{{login.orgId}}"}
	Variables map[string]interface{} `json:"variables"`
}

// SseFlowNode defines model for SseFlowNode.
type SseFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`

	// Data Consumes a `text/event-stream` endpoint, collecting events until the stream
	// ends, the event cap is hit, the completion event arrives, or the timeout
	// expires. The node's `assertions` run against each event as it arrives.
	Data SseNodeData `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen `json:"run_when,omitempty"`
	Type    SseFlowNodeType  `json:"type"`
}

// SseFlowNodeType defines model for SseFlowNode.Type.
type SseFlowNodeType string

// SseNodeData Consumes a `text/event-stream` endpoint, collecting events until the stream
// ends, the event cap is hit, the completion event arrives, or the timeout
// expires. The node's `assertions` run against each event as it arrives.
type SseNodeData struct {
	// CompletionEvent Stop as soon as an event whose `event:` name or raw data equals this value is
	// dispatched. Use it to end on a terminal event rather than waiting for EOF.
	//
	//
	// Examples: execution.completed
	CompletionEvent *string `json:"completion_event,omitempty"`

	// Headers Request headers. Values support variable substitution.
	//
	// Examples: {"Authorization":"Bearer {{auth.token}}"}
	Headers *map[string]string `json:"headers,omitempty"`

	// MaxEvents Stop after collecting this many events.
	MaxEvents *int `json:"max_events,omitempty"`

	// Method HTTP method used to open the stream.
	Method *HttpMethod `json:"method,omitempty"`

	// StopOnAssertionFailure Stop and fail the node on the first failing per-event assertion.
	StopOnAssertionFailure *bool `json:"stop_on_assertion_failure,omitempty"`

	// TimeoutMs Overall deadline for the stream, in milliseconds.
	TimeoutMs *int `json:"timeout_ms,omitempty"`

	// Url Event-stream endpoint. Supports the same variable substitution as a request node.
	//
	// Examples: {{apiUrl}}/flows/{{flowId}}/executions/{{launch.executionId}}/stream
	Url string `json:"url"`
}

// WebhookExpectation One event the wait expects, described by the assertions it must pass.
type WebhookExpectation struct {
	Assertions []CompositeAssertion `json:"assertions"`

	// Max Most events that may satisfy the group. Absent means no upper bound.
	Max *int `json:"max,omitempty"`

	// Min Fewest events that must satisfy the group. 0 with `max` 0 means the event must never arrive.
	Min *int `json:"min,omitempty"`

	// Name What the event is, in the words the run report uses.
	//
	// Examples: Invitation accepted
	Name string `json:"name"`
}

// WebhookWaitFlowNode defines model for WebhookWaitFlowNode.
type WebhookWaitFlowNode struct {
	// Assertions Validation assertions for the node
	Assertions *[]CompositeAssertion `json:"assertions,omitempty"`

	// Data Polls the webhook requests captured for this execution until a stored request passes the
	// node's assertions. Launch creates a webhook for this run and injects its ingest URL as `webhook.url`, and `webhook.requests_url` for this node. The webhook is deleted when the run ends. Default timeout is 30 seconds.
	// Assertion values resolve `{{node.output}}` templates from completed steps before they compare, so a
	// wait can name the resource an event must be about.
	Data WebhookWaitNodeData `json:"data"`

	// DisplayName Human-readable name for the node
	//
	// Examples: My Node
	DisplayName string `json:"display_name"`

	// Id Unique identifier for the node
	//
	// Examples: node-1
	Id string `json:"id"`

	// Outputs Named outputs extracted from the response/data
	Outputs *[]Output `json:"outputs,omitempty"`

	// RunWhen Controls whether a node runs only in the normal success path or also after the main flow has already failed.
	RunWhen *FlowNodeRunWhen        `json:"run_when,omitempty"`
	Type    WebhookWaitFlowNodeType `json:"type"`
}

// WebhookWaitFlowNodeType defines model for WebhookWaitFlowNode.Type.
type WebhookWaitFlowNodeType string

// WebhookWaitNodeData Polls the webhook requests captured for this execution until a stored request passes the
// node's assertions. Launch creates a webhook for this run and injects its ingest URL as `webhook.url`, and `webhook.requests_url` for this node. The webhook is deleted when the run ends. Default timeout is 30 seconds.
// Assertion values resolve `{{node.output}}` templates from completed steps before they compare, so a
// wait can name the resource an event must be about.
type WebhookWaitNodeData struct {
	// Expect The events this wait expects, each a named group of assertions. An event satisfies a group
	// when it passes every assertion of the group, and one event counts toward one group only.
	// With `expect`, the node's own `assertions` validate every event a group claimed. Without it,
	// the node's assertions pick the first request that passes them. The node result reports each
	// group in `expectations`, and for a group that found nothing, the closest request check by check.
	Expect *[]WebhookExpectation `json:"expect,omitempty"`

	// SettleMs How long the wait keeps listening once every expectation has its events, so an
	// expectation with a `max` sees an extra event that arrives late. Only used with `expect`.
	// Defaults to 0.
	//
	//
	// Examples: 3000
	SettleMs *int `json:"settle_ms,omitempty"`

	// TimeoutMs Wait budget in milliseconds. Defaults to 30000. Launch refuses a value above the execution `timeout_seconds` or the license `max_cloud_execution_duration_seconds`.
	//
	// Examples: 30000
	TimeoutMs *int `json:"timeout_ms,omitempty"`
}

func (t *FlowNode) FromRequestFlowNode(v RequestFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"request"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsRequestFlowNode() (RequestFlowNode, error) {
	var body RequestFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t *FlowNode) FromDelayFlowNode(v DelayFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"delay"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsDelayFlowNode() (DelayFlowNode, error) {
	var body DelayFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t *FlowNode) FromModuleFlowNode(v ModuleFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"module"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsModuleFlowNode() (ModuleFlowNode, error) {
	var body ModuleFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t *FlowNode) FromSetVariableFlowNode(v SetVariableFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"set_variable"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsSetVariableFlowNode() (SetVariableFlowNode, error) {
	var body SetVariableFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t *FlowNode) FromAssertFlowNode(v AssertFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"assert"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsAssertFlowNode() (AssertFlowNode, error) {
	var body AssertFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t *FlowNode) FromBranchFlowNode(v BranchFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"branch"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsBranchFlowNode() (BranchFlowNode, error) {
	var body BranchFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t *FlowNode) FromLoopFlowNode(v LoopFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"loop"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsLoopFlowNode() (LoopFlowNode, error) {
	var body LoopFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t *FlowNode) FromPollFlowNode(v PollFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"poll"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsPollFlowNode() (PollFlowNode, error) {
	var body PollFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t *FlowNode) FromSseFlowNode(v SseFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"sse"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsSseFlowNode() (SseFlowNode, error) {
	var body SseFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t *FlowNode) FromWebhookWaitFlowNode(v WebhookWaitFlowNode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err = jsonMerge(b, []byte(`{"type":"webhook_wait"}`))
	if err != nil {
		return err
	}
	t.union = b
	return nil
}

func (t FlowNode) AsWebhookWaitFlowNode() (WebhookWaitFlowNode, error) {
	var body WebhookWaitFlowNode
	err := json.Unmarshal(t.union, &body)
	return body, err
}

func (t FlowNode) MarshalJSON() ([]byte, error) {
	if t.union == nil {
		return []byte("null"), nil
	}
	return t.union.MarshalJSON()
}

func (t *FlowNode) UnmarshalJSON(b []byte) error {
	if t == nil {
		return errors.New("FlowNode: UnmarshalJSON on nil pointer")
	}
	return t.union.UnmarshalJSON(b)
}

func jsonMerge(dst, patch []byte) ([]byte, error) {
	var base map[string]json.RawMessage
	if err := json.Unmarshal(dst, &base); err != nil {
		return nil, err
	}
	var over map[string]json.RawMessage
	if err := json.Unmarshal(patch, &over); err != nil {
		return nil, err
	}
	for key, value := range over {
		base[key] = value
	}
	return json.Marshal(base)
}
