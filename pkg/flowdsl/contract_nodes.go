package flowdsl

import (
	"maps"
	"slices"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl/wire"

	"gopkg.in/yaml.v3"
)

func (g *graph) parseInput(node *dslNode, body *yaml.Node) {
	if g.inline {
		g.add(Error, lineOf(body), "node %q: input node is not allowed inside a loop or poll body", node.id)
		return
	}
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	spec := &inputSpec{}
	if reqNode, exists := m.take("required"); exists {
		list, err := stringList(reqNode)
		if err != nil {
			g.add(Error, lineOf(reqNode), "node %q required: %s", node.id, err.Error())
		} else {
			spec.required = list
		}
	}
	if propsNode, exists := m.take("properties"); exists {
		spec.properties = g.parseInputProperties(node, propsNode)
	}
	g.rejectNodeFields(node.id, m)
	node.input = spec
}

func (g *graph) parseInputProperties(node *dslNode, propsNode *yaml.Node) []inputPropSpec {
	pm, err := mappingOf(propsNode)
	if err != nil {
		g.add(Error, lineOf(propsNode), "node %q properties: %s", node.id, err.Error())
		return nil
	}
	out := make([]inputPropSpec, 0, len(pm.order))
	for _, name := range pm.order {
		prop, ok := g.parseOneInputProperty(node, name, pm.val[name])
		if ok {
			out = append(out, prop)
		}
	}
	return out
}

func (g *graph) parseOneInputProperty(node *dslNode, name string, raw *yaml.Node) (inputPropSpec, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		g.add(Error, lineOf(raw), "node %q input property name cannot be empty", node.id)
		return inputPropSpec{}, false
	}
	m, err := mappingOf(raw)
	if err != nil {
		g.add(Error, lineOf(raw), "node %q property %q: %s", node.id, name, err.Error())
		return inputPropSpec{}, false
	}
	prop := inputPropSpec{
		name:        trimmed,
		schemaType:  g.takeString(m, "type"),
		format:      g.takeString(m, "format"),
		description: g.takeString(m, "description"),
		enum:        g.takeAnyList(node, name, m, "enum"),
		required:    g.takeOptionalBool(node, name, m, "required"),
		secret:      g.takeOptionalBool(node, name, m, "secret"),
	}
	if defNode, exists := m.take("default"); exists && !isNull(defNode) {
		val, decodeErr := decodeAny(defNode)
		if decodeErr != nil {
			g.add(Error, lineOf(defNode), "node %q property %q default: %s", node.id, name, decodeErr.Error())
		} else {
			prop.hasDefault = true
			prop.defaultVal = val
		}
	}
	g.rejectNodeFields(node.id+"."+name, m)
	return prop, true
}

func (g *graph) parseOutput(node *dslNode, body *yaml.Node) {
	if g.inline {
		g.add(Error, lineOf(body), "node %q: output node is not allowed inside a loop or poll body", node.id)
		return
	}
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	spec := &outputSpec{}
	if propsNode, exists := m.take("properties"); exists {
		spec.properties = g.parseOutputProperties(node, propsNode)
	}
	g.rejectNodeFields(node.id, m)
	node.output = spec
}

func (g *graph) parseOutputProperties(node *dslNode, propsNode *yaml.Node) []outputPropSpec {
	pm, err := mappingOf(propsNode)
	if err != nil {
		g.add(Error, lineOf(propsNode), "node %q properties: %s", node.id, err.Error())
		return nil
	}
	out := make([]outputPropSpec, 0, len(pm.order))
	for _, name := range pm.order {
		prop, ok := g.parseOneOutputProperty(node, name, pm.val[name])
		if ok {
			out = append(out, prop)
		}
	}
	return out
}

func (g *graph) parseOneOutputProperty(node *dslNode, name string, raw *yaml.Node) (outputPropSpec, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		g.add(Error, lineOf(raw), "node %q output property name cannot be empty", node.id)
		return outputPropSpec{}, false
	}
	m, err := mappingOf(raw)
	if err != nil {
		g.add(Error, lineOf(raw), "node %q property %q: %s", node.id, name, err.Error())
		return outputPropSpec{}, false
	}
	valNode, hasVal := m.take("value")
	if !hasVal || isNull(valNode) {
		g.add(Error, lineOf(raw), "node %q output property %q needs a value", node.id, name)
		return outputPropSpec{}, false
	}
	val, decodeErr := decodeAny(valNode)
	if decodeErr != nil {
		g.add(Error, lineOf(valNode), "node %q property %q value: %s", node.id, name, decodeErr.Error())
		return outputPropSpec{}, false
	}
	prop := outputPropSpec{
		name:        trimmed,
		schemaType:  g.takeString(m, "type"),
		format:      g.takeString(m, "format"),
		description: g.takeString(m, "description"),
		enum:        g.takeAnyList(node, name, m, "enum"),
		value:       val,
		secret:      g.takeOptionalBool(node, name, m, "secret"),
	}
	g.rejectNodeFields(node.id+"."+name, m)
	return prop, true
}

func (g *graph) takeAnyList(node *dslNode, propName string, m ymap, key string) []any {
	raw, exists := m.take(key)
	if !exists {
		return nil
	}
	if raw.Kind != yaml.SequenceNode {
		g.add(Error, lineOf(raw), "node %q property %q %s must be a list", node.id, propName, key)
		return nil
	}
	items := make([]any, 0, len(raw.Content))
	for _, elem := range raw.Content {
		val, err := decodeAny(elem)
		if err != nil {
			g.add(Error, lineOf(elem), "node %q property %q %s: %s", node.id, propName, key, err.Error())
			continue
		}
		items = append(items, val)
	}
	return items
}

func (g *graph) takeOptionalBool(node *dslNode, propName string, m ymap, key string) bool {
	raw, exists := m.take(key)
	if !exists {
		return false
	}
	val, ok := asBool(raw)
	if !ok {
		g.add(Error, lineOf(raw), "node %q property %q %s must be true or false", node.id, propName, key)
		return false
	}
	return val
}

func lowerInput(n *dslNode) (wire.FlowNode, error) {
	props := make(map[string]wire.InputProperty, len(n.input.properties))
	for _, p := range n.input.properties {
		wp := wire.InputProperty{}
		if p.schemaType != "" {
			v := p.schemaType
			wp.Type = &v
		}
		if p.format != "" {
			v := p.format
			wp.Format = &v
		}
		if p.description != "" {
			v := p.description
			wp.Description = &v
		}
		if len(p.enum) > 0 {
			enum := append([]any{}, p.enum...)
			wp.Enum = &enum
		}
		if p.hasDefault {
			def := p.defaultVal
			wp.Default = &def
		}
		if p.required {
			req := true
			wp.Required = &req
		}
		if p.secret {
			sec := true
			wp.Secret = &sec
		}
		props[p.name] = wp
	}
	data := wire.InputNodeData{Properties: props}
	if len(n.input.required) > 0 {
		req := append([]string{}, n.input.required...)
		data.Required = &req
	}
	var out wire.FlowNode
	err := out.FromInputFlowNode(wire.InputFlowNode{
		Id:          n.id,
		Type:        wire.InputFlowNodeTypeInput,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Data:        data,
	})
	return out, err
}

func lowerOutput(n *dslNode) (wire.FlowNode, error) {
	props := make(map[string]wire.OutputProperty, len(n.output.properties))
	for _, p := range n.output.properties {
		wp := wire.OutputProperty{Value: p.value}
		if p.schemaType != "" {
			v := p.schemaType
			wp.Type = &v
		}
		if p.format != "" {
			v := p.format
			wp.Format = &v
		}
		if p.description != "" {
			v := p.description
			wp.Description = &v
		}
		if len(p.enum) > 0 {
			enum := append([]any{}, p.enum...)
			wp.Enum = &enum
		}
		if p.secret {
			sec := true
			wp.Secret = &sec
		}
		props[p.name] = wp
	}
	var out wire.FlowNode
	err := out.FromOutputFlowNode(wire.OutputFlowNode{
		Id:          n.id,
		Type:        wire.OutputFlowNodeTypeOutput,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Data:        wire.OutputNodeData{Properties: props},
	})
	return out, err
}

func addInput(body *mapBuilder, node wire.FlowNode) error {
	spec, err := node.AsInputFlowNode()
	if err != nil {
		return err
	}
	inMap := newMap()
	if spec.Data.Required != nil && len(*spec.Data.Required) > 0 {
		inMap.raw("required", stringSeq(*spec.Data.Required))
	}
	propsMap := newMap()
	for _, name := range slices.Sorted(maps.Keys(spec.Data.Properties)) {
		propNode, propErr := renderInputProperty(spec.Data.Properties[name])
		if propErr != nil {
			return propErr
		}
		propsMap.raw(name, propNode)
	}
	inMap.raw("properties", propsMap.node)
	body.raw(kindInput, inMap.node)
	return nil
}

func renderInputProperty(p wire.InputProperty) (*yaml.Node, error) {
	pm := newMap()
	if p.Type != nil && *p.Type != "" {
		pm.str("type", *p.Type)
	}
	if p.Format != nil && *p.Format != "" {
		pm.str("format", *p.Format)
	}
	if p.Description != nil && *p.Description != "" {
		pm.str("description", *p.Description)
	}
	if p.Enum != nil && len(*p.Enum) > 0 {
		enumNode, err := anySeqNode(*p.Enum)
		if err != nil {
			return nil, err
		}
		pm.raw("enum", enumNode)
	}
	if p.Default != nil {
		defNode, err := valueNode(*p.Default)
		if err != nil {
			return nil, err
		}
		pm.raw("default", defNode)
	}
	if p.Required != nil && *p.Required {
		pm.bool("required", true)
	}
	if p.Secret != nil && *p.Secret {
		pm.bool("secret", true)
	}
	return pm.node, nil
}

func addOutput(body *mapBuilder, node wire.FlowNode) error {
	spec, err := node.AsOutputFlowNode()
	if err != nil {
		return err
	}
	outMap := newMap()
	propsMap := newMap()
	for _, name := range slices.Sorted(maps.Keys(spec.Data.Properties)) {
		propNode, propErr := renderOutputProperty(spec.Data.Properties[name])
		if propErr != nil {
			return propErr
		}
		propsMap.raw(name, propNode)
	}
	outMap.raw("properties", propsMap.node)
	body.raw(kindOutput, outMap.node)
	return nil
}

func renderOutputProperty(p wire.OutputProperty) (*yaml.Node, error) {
	pm := newMap()
	if p.Type != nil && *p.Type != "" {
		pm.str("type", *p.Type)
	}
	if p.Format != nil && *p.Format != "" {
		pm.str("format", *p.Format)
	}
	if p.Description != nil && *p.Description != "" {
		pm.str("description", *p.Description)
	}
	if p.Enum != nil && len(*p.Enum) > 0 {
		enumNode, err := anySeqNode(*p.Enum)
		if err != nil {
			return nil, err
		}
		pm.raw("enum", enumNode)
	}
	valNode, err := valueNode(p.Value)
	if err != nil {
		return nil, err
	}
	pm.raw("value", valNode)
	if p.Secret != nil && *p.Secret {
		pm.bool("secret", true)
	}
	return pm.node, nil
}
