package flowdsl

import (
	"fmt"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl/wire"

	"github.com/google/uuid"
)

func (g *graph) lower() (wire.CreateFlowRequest, error) {
	nodes := make([]wire.FlowNode, 0, len(g.nodes))
	for _, n := range g.nodes {
		one, err := lowerNode(n)
		if err != nil {
			return wire.CreateFlowRequest{}, err
		}
		nodes = append(nodes, one)
	}
	auto := true
	version := g.version
	definition := wire.FlowDefinition{
		Name:    g.name,
		Version: version,
		Nodes:   nodes,
		Edges:   lowerEdges(g.nodes),
	}
	request := wire.CreateFlowRequest{
		Name:           g.name,
		AutoLayout:     &auto,
		FlowDefinition: definition,
		Version:        &version,
	}
	if g.description != "" {
		description := g.description
		request.Description = &description
		request.FlowDefinition.Description = &description
	}
	if g.role != "" {
		role := wire.FlowRole(g.role)
		request.Role = &role
	}
	if len(g.tags) > 0 {
		tags := append([]string{}, g.tags...)
		request.Tags = &tags
	}
	if id := folderUUID(g.folder); id != nil {
		request.FolderId = id
	}
	return request, nil
}

func folderUUID(value string) *uuid.UUID {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func lowerEdges(nodes []*dslNode) []wire.FlowEdge {
	edges := make([]wire.FlowEdge, 0)
	seen := map[string]bool{}
	for _, n := range nodes {
		for _, target := range n.next {
			addEdge(&edges, seen, n.id, target, wire.Success)
		}
		addBranchEdges(&edges, seen, n)
		for _, target := range n.fail {
			addEdge(&edges, seen, n.id, target, wire.Failure)
		}
	}
	return edges
}

func addBranchEdges(edges *[]wire.FlowEdge, seen map[string]bool, n *dslNode) {
	if n.branch == nil {
		return
	}
	for _, item := range n.branch.cases {
		addEdge(edges, seen, n.id, item.target, wire.Success)
	}
	if n.branch.hasDefault {
		addEdge(edges, seen, n.id, n.branch.defaultTarget, wire.Success)
	}
}

func addEdge(edges *[]wire.FlowEdge, seen map[string]bool, source, target string, kind wire.FlowEdgeType) {
	if target == "" {
		return
	}
	key := source + "\x00" + target + "\x00" + string(kind)
	if seen[key] {
		return
	}
	seen[key] = true
	id := source + "-" + target
	if kind == wire.Failure {
		id += "-failure"
	}
	*edges = append(*edges, wire.FlowEdge{Id: id, Source: source, Target: target, Type: kind})
}

func lowerInline(g *graph) (wire.InlineFlowDefinition, error) {
	nodes := make([]wire.FlowNode, 0, len(g.nodes))
	for _, n := range g.nodes {
		one, err := lowerNode(n)
		if err != nil {
			return wire.InlineFlowDefinition{}, err
		}
		nodes = append(nodes, one)
	}
	def := wire.InlineFlowDefinition{Nodes: nodes}
	if edges := lowerEdges(g.nodes); len(edges) > 0 {
		def.Edges = &edges
	}
	return def, nil
}

func lowerNode(n *dslNode) (wire.FlowNode, error) {
	switch n.kind {
	case kindRequest:
		return lowerRequest(n)
	case kindModule:
		return lowerModule(n)
	case kindDelay:
		return lowerDelay(n)
	case kindSet:
		return lowerSet(n)
	case kindAssert:
		return lowerAssert(n)
	case kindBranch:
		return lowerBranch(n)
	case kindPoll:
		return lowerPoll(n)
	case kindLoop:
		return lowerLoop(n)
	case kindWebhook:
		return lowerWebhook(n)
	case kindSSE:
		return lowerSSE(n)
	default:
		return wire.FlowNode{}, fmt.Errorf("node %q has no kind", n.id)
	}
}

func baseAssertions(n *dslNode) *[]wire.CompositeAssertion {
	if len(n.expects) == 0 {
		return nil
	}
	copied := append([]wire.CompositeAssertion{}, n.expects...)
	return &copied
}

func runWhenOf(value string) *wire.FlowNodeRunWhen {
	mode := wire.OnSuccess
	if value == string(wire.Always) {
		mode = wire.Always
	}
	return &mode
}

func lowerRequest(n *dslNode) (wire.FlowNode, error) {
	data := wire.RequestNodeData{
		Method:      wire.HttpMethod(strings.ToUpper(n.request.method)),
		Url:         n.request.url,
		Headers:     stringMap(n.request.headers),
		QueryParams: anyMap(n.request.query),
		Timeout:     n.request.timeout,
	}
	if n.request.hasBody {
		data.Body = n.request.body
	}
	var node wire.FlowNode
	err := node.FromRequestFlowNode(wire.RequestFlowNode{
		Id:          n.id,
		Type:        wire.RequestFlowNodeTypeRequest,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Outputs:     extractorOutputs(n.outputs),
		Data:        data,
	})
	return node, err
}

func lowerModule(n *dslNode) (wire.FlowNode, error) {
	parsed, err := uuid.Parse(n.module.flowID)
	if err != nil {
		return wire.FlowNode{}, fmt.Errorf("node %q has no flow id", n.id)
	}
	data := wire.ModuleNodeData{FlowId: parsed}
	if inputs := anyMap(n.module.inputs); inputs != nil {
		data.InputBindings = inputs
	}
	if bindings := bindingMap(n.outputs); bindings != nil {
		data.OutputBindings = bindings
	}
	var node wire.FlowNode
	err = node.FromModuleFlowNode(wire.ModuleFlowNode{
		Id:          n.id,
		Type:        wire.ModuleFlowNodeTypeModule,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data:        data,
	})
	return node, err
}

func lowerDelay(n *dslNode) (wire.FlowNode, error) {
	var node wire.FlowNode
	err := node.FromDelayFlowNode(wire.DelayFlowNode{
		Id:          n.id,
		Type:        wire.DelayFlowNodeTypeDelay,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data:        wire.DelayNodeData{Duration: n.delayMS},
	})
	return node, err
}

func lowerSet(n *dslNode) (wire.FlowNode, error) {
	var node wire.FlowNode
	err := node.FromSetVariableFlowNode(wire.SetVariableFlowNode{
		Id:          n.id,
		Type:        wire.SetVariableFlowNodeTypeSetVariable,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data:        wire.SetVariableNodeData{Variables: valueMap(n.vars)},
	})
	return node, err
}

func lowerAssert(n *dslNode) (wire.FlowNode, error) {
	data := wire.AssertNodeData{}
	if n.assert != nil && n.assert.set {
		data.Target = n.assert.target
	}
	var node wire.FlowNode
	err := node.FromAssertFlowNode(wire.AssertFlowNode{
		Id:          n.id,
		Type:        wire.AssertFlowNodeTypeAssert,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Outputs:     extractorOutputs(n.outputs),
		Data:        data,
	})
	return node, err
}

func lowerBranch(n *dslNode) (wire.FlowNode, error) {
	data := wire.BranchNodeData{Cases: make([]wire.BranchCase, 0, len(n.branch.cases))}
	if n.branch.targetSet {
		data.Target = n.branch.target
	}
	for _, item := range n.branch.cases {
		data.Cases = append(data.Cases, wire.BranchCase{Target: item.target, When: item.when})
	}
	if n.branch.hasDefault {
		target := n.branch.defaultTarget
		data.Default = &target
	}
	var node wire.FlowNode
	err := node.FromBranchFlowNode(wire.BranchFlowNode{
		Id:          n.id,
		Type:        wire.BranchFlowNodeTypeBranch,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data:        data,
	})
	return node, err
}

func lowerPoll(n *dslNode) (wire.FlowNode, error) {
	if n.poll.body == nil {
		return wire.FlowNode{}, fmt.Errorf("node %q needs a body", n.id)
	}
	body, err := lowerInline(n.poll.body)
	if err != nil {
		return wire.FlowNode{}, err
	}
	var node wire.FlowNode
	err = node.FromPollFlowNode(wire.PollFlowNode{
		Id:          n.id,
		Type:        wire.PollFlowNodeTypePoll,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data: wire.PollNodeData{
			Body:        body,
			MaxAttempts: n.poll.maxAttempts,
			IntervalMs:  n.poll.interval,
			TimeoutMs:   n.poll.timeout,
		},
	})
	return node, err
}

func lowerLoop(n *dslNode) (wire.FlowNode, error) {
	if n.loop.body == nil {
		return wire.FlowNode{}, fmt.Errorf("node %q needs a body", n.id)
	}
	body, err := lowerInline(n.loop.body)
	if err != nil {
		return wire.FlowNode{}, err
	}
	var node wire.FlowNode
	err = node.FromLoopFlowNode(wire.LoopFlowNode{
		Id:          n.id,
		Type:        wire.LoopFlowNodeTypeLoop,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data: wire.LoopNodeData{
			Body:            body,
			Items:           n.loop.items,
			ItemVar:         strPtr(n.loop.itemVar),
			IndexVar:        strPtr(n.loop.indexVar),
			MaxIterations:   n.loop.max,
			ContinueOnError: n.loop.continueOn,
		},
	})
	return node, err
}

func lowerWebhook(n *dslNode) (wire.FlowNode, error) {
	data := wire.WebhookWaitNodeData{TimeoutMs: n.hook.timeout, SettleMs: n.hook.settle}
	if groups := hookGroups(n.hook.groups); groups != nil {
		data.Expect = groups
	}
	var node wire.FlowNode
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

func hookGroups(groups []hookGroup) *[]wire.WebhookExpectation {
	if len(groups) == 0 {
		return nil
	}
	out := make([]wire.WebhookExpectation, 0, len(groups))
	for _, group := range groups {
		item := wire.WebhookExpectation{Name: group.name, Min: group.min, Max: group.max}
		if len(group.checks) > 0 {
			item.Assertions = append([]wire.CompositeAssertion{}, group.checks...)
		} else {
			item.Assertions = []wire.CompositeAssertion{}
		}
		out = append(out, item)
	}
	return &out
}

func lowerSSE(n *dslNode) (wire.FlowNode, error) {
	data := wire.SseNodeData{
		Url:                    n.sse.url,
		Headers:                stringMap(n.sse.headers),
		TimeoutMs:              n.sse.timeout,
		MaxEvents:              n.sse.maxEvents,
		CompletionEvent:        n.sse.completion,
		StopOnAssertionFailure: n.sse.stop,
	}
	if n.sse.method != "" {
		method := wire.HttpMethod(strings.ToUpper(n.sse.method))
		data.Method = &method
	}
	var node wire.FlowNode
	err := node.FromSseFlowNode(wire.SseFlowNode{
		Id:          n.id,
		Type:        wire.SseFlowNodeTypeSse,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Outputs:     extractorOutputs(n.outputs),
		Data:        data,
	})
	return node, err
}

func extractorOutputs(decls []outDecl) *[]wire.Output {
	if len(decls) == 0 {
		return nil
	}
	out := make([]wire.Output, 0, len(decls))
	for _, decl := range decls {
		item := wire.Output{Name: decl.name, Secret: secretPtr(decl.secret)}
		switch decl.extractor {
		case string(wire.ExtractorTypeHeader):
			header := decl.header
			item.Extractor.Type = wire.ExtractorTypeHeader
			item.Extractor.HeaderName = &header
		case string(wire.ExtractorTypeBody):
			item.Extractor.Type = wire.ExtractorTypeBody
		case string(wire.ExtractorTypeStatusCode):
			item.Extractor.Type = wire.ExtractorTypeStatusCode
		default:
			item.Extractor.Type = wire.ExtractorTypeJsonPath
			path := decl.path
			item.Extractor.Path = &path
		}
		out = append(out, item)
	}
	return &out
}

func secretPtr(on bool) *bool {
	if !on {
		return nil
	}
	value := true
	return &value
}

func strPtr(value string) *string {
	if value == "" {
		return nil
	}
	text := value
	return &text
}

func stringMap(pairs []pair) *map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, item := range pairs {
		text, ok := item.val.(string)
		if !ok {
			continue
		}
		out[item.key] = text
	}
	return &out
}

func anyMap(pairs []pair) *map[string]any {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]any, len(pairs))
	for _, item := range pairs {
		out[item.key] = item.val
	}
	return &out
}

func valueMap(pairs []pair) map[string]any {
	out := make(map[string]any, len(pairs))
	for _, item := range pairs {
		out[item.key] = item.val
	}
	return out
}

func bindingMap(decls []outDecl) *map[string]string {
	if len(decls) == 0 {
		return nil
	}
	out := make(map[string]string, len(decls))
	for _, decl := range decls {
		if decl.binding == "" {
			continue
		}
		out[decl.name] = decl.binding
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}
