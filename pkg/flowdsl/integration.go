package flowdsl

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl/wire"

	"gopkg.in/yaml.v3"
)

// launchFilled are the request fields launch fills from an integration operation.
var launchFilled = []string{"method", "url", "body", "headers"}

// parseIntegration reads an integration node. An operation becomes a request
// node and an event becomes a webhook_wait node; launch fills the rest from
// the integration. The rules match the MCP build_flow tool.
func (g *graph) parseIntegration(node *dslNode, body *yaml.Node) {
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	for _, key := range launchFilled {
		if _, set := m.val[key]; set {
			g.add(Error, m.keyLine(key),
				"integration node %q: remove method, url, body and headers: launch fills them from the integration",
				node.id)
			break
		}
	}
	for _, key := range launchFilled {
		delete(m.val, key)
	}
	spec := &integSpec{
		id:        strings.TrimSpace(g.takeString(m, "id")),
		operation: strings.TrimSpace(g.takeString(m, "operation")),
		event:     strings.TrimSpace(g.takeString(m, "event")),
	}
	if spec.id == "" {
		g.add(Error, orLine(m, "id"), "integration node %q: integration.id is required, such as stripe", node.id)
	}
	if (spec.operation == "") == (spec.event == "") {
		g.add(
			Error,
			m.line,
			"integration node %q: set exactly one of integration.operation and integration.event",
			node.id,
		)
	}
	if raw, exists := m.take("inputs"); exists {
		if spec.isEvent() {
			g.add(Error, lineOf(raw), "integration node %q: an event takes no inputs", node.id)
		} else {
			spec.inputs = g.stringPairs(raw)
		}
	}
	spec.timeout = g.takePositive(node, m, "timeout_ms")
	if spec.isEvent() {
		spec.settle = g.takeNonNegative(node, m, "settle_ms")
		if raw, exists := m.take("expect"); exists {
			spec.groups = g.parseHookGroups(node, raw)
		}
	}
	g.rejectNodeFields(node.id, m)
	node.integ = spec
}

// checkIntegrationExpects refuses a status check on an event: a webhook event
// has a payload and no status code.
func (g *graph) checkIntegrationExpects(node *dslNode, line int) {
	if node.integ == nil || !node.integ.isEvent() {
		return
	}
	for i, check := range node.expects {
		if check.ExtractorType == wire.ExtractorTypeStatusCode {
			g.add(Error, line, "integration node %q: assert #%d: an event has no status; assert on its payload path",
				node.id, i+1)
		}
	}
}

func lowerIntegration(n *dslNode) (wire.FlowNode, error) {
	spec := n.integ
	ref := &wire.IntegrationNodeRef{Id: spec.id}
	var node wire.FlowNode
	if spec.isEvent() {
		event := spec.event
		ref.Event = &event
		data := wire.WebhookWaitNodeData{Integration: ref, TimeoutMs: spec.timeout, SettleMs: spec.settle}
		if groups := hookGroups(spec.groups); groups != nil {
			data.Expect = groups
		}
		err := node.FromWebhookWaitFlowNode(wire.WebhookWaitFlowNode{
			Id:          n.id,
			Type:        wire.WebhookWaitFlowNodeTypeWebhookWait,
			DisplayName: n.display,
			RunWhen:     runWhenOf(n.runWhen),
			Assertions:  baseAssertions(n),
			Outputs:     extractorOutputs(n.outputs),
			Data:        data,
		})
		return node, err
	}
	operation := spec.operation
	ref.Operation = &operation
	ref.Inputs = anyMap(spec.inputs)
	err := node.FromRequestFlowNode(wire.RequestFlowNode{
		Id:          n.id,
		Type:        wire.RequestFlowNodeTypeRequest,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Outputs:     extractorOutputs(n.outputs),
		Data:        wire.RequestNodeData{Integration: ref, Timeout: spec.timeout},
	})
	return node, err
}

// IntegrationNode is one integration node of a compiled flow.
type IntegrationNode struct {
	// NodeID is the node's id. A node inside a poll or loop body is named
	// parent/child.
	NodeID string
	Ref    wire.IntegrationNodeRef
}

// IsEvent reports whether the node waits for an event rather than calling an operation.
func (n IntegrationNode) IsEvent() bool {
	return n.Ref.Event != nil && *n.Ref.Event != ""
}

// Target is the HTTP call an integration operation makes.
type Target struct {
	Method string
	URL    string
}

// FillIntegrations calls resolve for every integration node of req, poll and
// loop bodies included. The compiler works offline, so an operation node
// leaves it without a method and url; the API requires both. For an operation,
// resolve returns the target to store on the node; for an event, the target is
// ignored. An error from resolve refuses that node; FillIntegrations returns
// every refusal joined.
func FillIntegrations(req *wire.CreateFlowRequest, resolve func(IntegrationNode) (Target, error)) error {
	return fillNodes(req.FlowDefinition.Nodes, "", resolve)
}

func fillNodes(nodes []wire.FlowNode, prefix string, resolve func(IntegrationNode) (Target, error)) error {
	var errs []error
	for i := range nodes {
		if err := fillNode(&nodes[i], prefix, resolve); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func fillNode(node *wire.FlowNode, prefix string, resolve func(IntegrationNode) (Target, error)) error {
	raw, err := json.Marshal(node)
	if err != nil {
		return err
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return err
	}
	switch head.Type {
	case kindRequest:
		spec, err := node.AsRequestFlowNode()
		if err != nil || spec.Data.Integration == nil {
			return err
		}
		target, err := resolve(IntegrationNode{NodeID: prefix + spec.Id, Ref: *spec.Data.Integration})
		if err != nil {
			return err
		}
		spec.Data.Method = wire.HttpMethod(strings.ToUpper(target.Method))
		spec.Data.Url = target.URL
		return node.FromRequestFlowNode(spec)
	case kindWebhook:
		spec, err := node.AsWebhookWaitFlowNode()
		if err != nil || spec.Data.Integration == nil {
			return err
		}
		_, err = resolve(IntegrationNode{NodeID: prefix + spec.Id, Ref: *spec.Data.Integration})
		return err
	case kindPoll:
		spec, err := node.AsPollFlowNode()
		if err != nil {
			return err
		}
		if err := fillNodes(spec.Data.Body.Nodes, prefix+spec.Id+"/", resolve); err != nil {
			return err
		}
		return node.FromPollFlowNode(spec)
	case kindLoop:
		spec, err := node.AsLoopFlowNode()
		if err != nil {
			return err
		}
		if err := fillNodes(spec.Data.Body.Nodes, prefix+spec.Id+"/", resolve); err != nil {
			return err
		}
		return node.FromLoopFlowNode(spec)
	}
	return nil
}

// addIntegrationRequest renders a request node an integration fills. The
// stored method and url only describe the call, so the file leaves them out.
func addIntegrationRequest(body *mapBuilder, data wire.RequestNodeData) error {
	ref := data.Integration
	integ := newMap()
	integ.str("id", ref.Id)
	if ref.Operation != nil {
		integ.str("operation", *ref.Operation)
	}
	if ref.Event != nil && *ref.Event != "" {
		return fmt.Errorf("integration request %q sets an event", ref.Id)
	}
	if ref.Inputs != nil && len(*ref.Inputs) > 0 {
		inputs, err := anyMapNode(*ref.Inputs)
		if err != nil {
			return err
		}
		integ.raw("inputs", inputs)
	}
	addInt(integ, "timeout_ms", data.Timeout)
	body.raw(kindIntegration, integ.node)
	return nil
}

func addIntegrationWait(body *mapBuilder, data wire.WebhookWaitNodeData) error {
	ref := data.Integration
	integ := newMap()
	integ.str("id", ref.Id)
	if ref.Event != nil {
		integ.str("event", *ref.Event)
	}
	if ref.Operation != nil && *ref.Operation != "" {
		return fmt.Errorf("integration wait %q sets an operation", ref.Id)
	}
	if ref.Inputs != nil && len(*ref.Inputs) > 0 {
		return fmt.Errorf("integration wait %q sets inputs", ref.Id)
	}
	addInt(integ, "timeout_ms", data.TimeoutMs)
	addInt(integ, "settle_ms", data.SettleMs)
	if data.Expect != nil {
		groups, err := hookExpect(*data.Expect)
		if err != nil {
			return err
		}
		integ.raw("expect", groups)
	}
	body.raw(kindIntegration, integ.node)
	return nil
}
