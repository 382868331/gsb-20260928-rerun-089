package main

import (
	"errors"
	"fmt"
	"sort"
)

const scanIntervalMs = 100

var nodeKinds = map[string]bool{
	"INPUT":  true,
	"OUTPUT": true,
	"AND":    true,
	"OR":     true,
	"NOT":    true,
	"SR":     true,
	"TON":    true,
}

var inputPorts = map[string][]string{
	"OUTPUT": {"in"},
	"AND":    {"a", "b"},
	"OR":     {"a", "b"},
	"NOT":    {"in"},
	"SR":     {"set", "reset"},
	"TON":    {"in"},
}

func compileProgram(program Program) (*CompiledProgram, error) {
	if len(program.Nodes) == 0 {
		return nil, errors.New("程序至少需要一个节点")
	}
	if len(program.Edges) == 0 {
		return nil, errors.New("程序至少需要一条连接")
	}

	nodes := make(map[string]Node, len(program.Nodes))
	compiled := &CompiledProgram{
		program: program,
		nodes:   nodes,
		inputs:  map[string]bool{},
		sources: map[string]map[string]PortRef{},
	}

	for _, node := range program.Nodes {
		if node.ID == "" || node.Label == "" {
			return nil, errors.New("节点必须包含 id 和 label")
		}
		if _, exists := nodes[node.ID]; exists {
			return nil, fmt.Errorf("节点 id 重复：%s", node.ID)
		}
		if !nodeKinds[node.Kind] {
			return nil, fmt.Errorf("节点 %s 使用了不支持的类型 %s", node.ID, node.Kind)
		}
		if node.Kind == "TON" && node.PresetMs <= 0 {
			return nil, fmt.Errorf("定时器 %s 的 presetMs 必须大于 0", node.ID)
		}
		nodes[node.ID] = node
		compiled.sources[node.ID] = map[string]PortRef{}
		if node.Kind == "INPUT" {
			compiled.inputs[node.ID] = false
		}
	}

	hasOutput := false
	for _, node := range nodes {
		if node.Kind == "OUTPUT" {
			hasOutput = true
		}
	}
	if !hasOutput {
		return nil, errors.New("程序至少需要一个 OUTPUT 节点")
	}

	edgeIDs := map[string]bool{}
	for _, edge := range program.Edges {
		if edge.ID == "" || edge.Source == "" || edge.Target == "" {
			return nil, errors.New("连线必须包含 id、source 和 target")
		}
		if edgeIDs[edge.ID] {
			return nil, fmt.Errorf("连线 id 重复：%s", edge.ID)
		}
		edgeIDs[edge.ID] = true
		source, sourceOK := nodes[edge.Source]
		target, targetOK := nodes[edge.Target]
		if !sourceOK || !targetOK {
			return nil, fmt.Errorf("连线 %s 引用了不存在的节点", edge.ID)
		}
		if source.Kind == "OUTPUT" {
			return nil, fmt.Errorf("OUTPUT 节点 %s 不能作为信号源", source.ID)
		}
		allowedInputs, ok := inputPorts[target.Kind]
		if !ok || !contains(allowedInputs, edge.TargetPort) {
			return nil, fmt.Errorf("节点 %s 没有输入端口 %s", target.ID, edge.TargetPort)
		}
		if edge.SourcePort != "q" {
			return nil, fmt.Errorf("节点 %s 只有 q 输出端口", source.ID)
		}
		if previous, exists := compiled.sources[target.ID][edge.TargetPort]; exists {
			return nil, fmt.Errorf("节点 %s 的端口 %s 已经连接到 %s", target.ID, edge.TargetPort, previous.Node)
		}
		compiled.sources[target.ID][edge.TargetPort] = PortRef{Node: source.ID, Port: "q"}
	}

	for _, node := range nodes {
		for _, port := range inputPorts[node.Kind] {
			if _, ok := compiled.sources[node.ID][port]; !ok {
				return nil, fmt.Errorf("节点 %s 的输入端口 %s 未连接", node.ID, port)
			}
		}
	}

	order, err := topologicalOrder(nodes, compiled.sources)
	if err != nil {
		return nil, err
	}
	compiled.order = order
	return compiled, nil
}

func topologicalOrder(nodes map[string]Node, sources map[string]map[string]PortRef) ([]string, error) {
	permanent := map[string]bool{}
	temporary := map[string]bool{}
	var order []string
	var visit func(string) error
	visit = func(id string) error {
		if permanent[id] {
			return nil
		}
		if temporary[id] {
			return fmt.Errorf("节点 %s 位于反馈环路中，工业连线不允许循环", id)
		}
		temporary[id] = true
		portRefs := make([]PortRef, 0, len(sources[id]))
		for _, ref := range sources[id] {
			portRefs = append(portRefs, ref)
		}
		sort.Slice(portRefs, func(i, j int) bool {
			return portRefs[i].Node < portRefs[j].Node
		})
		for _, ref := range portRefs {
			if err := visit(ref.Node); err != nil {
				return err
			}
		}
		temporary[id] = false
		permanent[id] = true
		order = append(order, id)
		return nil
	}
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type Controller struct {
	mode    string
	version int
	program *CompiledProgram
	inputs  map[string]bool
	values  map[string]bool
	timers  map[string]TimerState
	scans   int
	log     []ScanEvent
}

func NewController(program Program) (*Controller, error) {
	compiled, err := compileProgram(program)
	if err != nil {
		return nil, err
	}
	controller := &Controller{
		mode:    "stopped",
		version: 1,
		program: compiled,
		inputs:  map[string]bool{},
		values:  map[string]bool{},
		timers:  map[string]TimerState{},
	}
	for id := range compiled.inputs {
		controller.inputs[id] = false
	}
	for _, node := range compiled.nodes {
		if node.Kind == "TON" {
			controller.timers[node.ID] = TimerState{}
		}
	}
	controller.addLog("新版本程序已载入，等待启动")
	return controller, nil
}

func (c *Controller) Load(program Program, keepCompatible bool) error {
	compiled, err := compileProgram(program)
	if err != nil {
		return err
	}
	previous := c.program
	c.program = compiled
	c.version++
	if !keepCompatible {
		c.mode = "stopped"
		c.inputs = map[string]bool{}
		c.values = map[string]bool{}
		c.timers = map[string]TimerState{}
		c.scans = 0
		c.log = nil
		for id := range compiled.inputs {
			c.inputs[id] = false
		}
		for _, node := range compiled.nodes {
			if node.Kind == "TON" {
				c.timers[node.ID] = TimerState{}
			}
		}
		c.addLog("编辑后的程序已作为新版本载入，运行状态已明确重置")
		return nil
	}
	inputs := map[string]bool{}
	for id := range compiled.inputs {
		inputs[id] = c.inputs[id]
	}
	values := map[string]bool{}
	for id, node := range compiled.nodes {
		if old, ok := previous.nodes[id]; ok && old.Kind == node.Kind {
			values[id] = c.values[id]
		}
	}
	timers := map[string]TimerState{}
	for id, node := range compiled.nodes {
		if node.Kind != "TON" {
			continue
		}
		if old, ok := previous.nodes[id]; ok && old.Kind == "TON" && old.PresetMs == node.PresetMs {
			timers[id] = c.timers[id]
		}
	}
	c.inputs = inputs
	c.values = values
	c.timers = timers
	c.addLog("新版本已载入并保留可兼容状态；被删除或修改的元件状态已清除")
	return nil
}

func (c *Controller) Preview(program Program) (*RevisionImpact, error) {
	compiled, err := compileProgram(program)
	if err != nil {
		return nil, err
	}
	impact := &RevisionImpact{
		Compatible:    true,
		TimerChanges:  []ImpactItem{},
		RenamedInputs: []ImpactItem{},
		RemovedNodes:  []ImpactItem{},
		AddedNodes:    []ImpactItem{},
	}
	for id, node := range compiled.nodes {
		old, ok := c.program.nodes[id]
		if !ok {
			impact.AddedNodes = append(impact.AddedNodes, ImpactItem{ID: id, Label: node.Label, Detail: fmt.Sprintf("新增 %s 元件，状态从零开始", node.Kind)})
			continue
		}
		if old.Kind != node.Kind {
			impact.Compatible = false
			impact.RemovedNodes = append(impact.RemovedNodes, ImpactItem{ID: id, Label: old.Label, Detail: fmt.Sprintf("类型由 %s 变为 %s，原状态无法保留", old.Kind, node.Kind)})
			continue
		}
		if node.Kind == "TON" && node.PresetMs != old.PresetMs {
			impact.TimerChanges = append(impact.TimerChanges, ImpactItem{ID: id, Label: node.Label, Detail: fmt.Sprintf("互锁延时 %dms -> %dms，保留状态时该定时器累计清零", old.PresetMs, node.PresetMs)})
		}
		if node.Kind == "INPUT" && node.Label != old.Label {
			impact.RenamedInputs = append(impact.RenamedInputs, ImpactItem{ID: id, Label: node.Label, Detail: fmt.Sprintf("输入 %s 重命名为 %s，采样值保持不变", old.Label, node.Label)})
		}
	}
	for id, node := range c.program.nodes {
		if _, ok := compiled.nodes[id]; !ok {
			impact.RemovedNodes = append(impact.RemovedNodes, ImpactItem{ID: id, Label: node.Label, Detail: fmt.Sprintf("%s 元件被删除，其状态与连接一并移除", node.Kind)})
		}
	}
	return impact, nil
}

func (c *Controller) Start() {
	if c.mode == "running" {
		return
	}
	c.mode = "running"
	c.addLog("扫描已启动；暂停不会重置内部状态")
}

func (c *Controller) Pause() {
	if c.mode == "running" {
		c.mode = "paused"
		c.addLog("扫描已暂停，定时累计值保留")
	}
}

func (c *Controller) Stop() {
	if c.mode == "stopped" {
		return
	}
	c.mode = "stopped"
	c.values = map[string]bool{}
	c.timers = map[string]TimerState{}
	for _, node := range c.program.nodes {
		if node.Kind == "TON" {
			c.timers[node.ID] = TimerState{}
		}
	}
	c.scans = 0
	c.addLog("扫描已停止，锁存与定时状态复位")
}

func (c *Controller) SetInput(id string, value bool) error {
	if _, ok := c.inputs[id]; !ok {
		return fmt.Errorf("输入 %s 不存在", id)
	}
	if c.inputs[id] != value {
		c.inputs[id] = value
		c.addLog(fmt.Sprintf("输入 %s 采样值=%t", id, value))
	}
	return nil
}

func (c *Controller) Scan() ControllerState {
	if c.mode != "running" {
		return c.State()
	}
	c.scans++
	previousOutputs := c.outputSnapshot()
	nextValues := map[string]bool{}
	nextTimers := map[string]TimerState{}
	for id, timer := range c.timers {
		nextTimers[id] = timer
	}

	for _, id := range c.program.order {
		node := c.program.nodes[id]
		switch node.Kind {
		case "INPUT":
			nextValues[id] = c.inputs[id]
		case "AND":
			nextValues[id] = c.sourceValue(nextValues, id, "a") && c.sourceValue(nextValues, id, "b")
		case "OR":
			nextValues[id] = c.sourceValue(nextValues, id, "a") || c.sourceValue(nextValues, id, "b")
		case "NOT":
			nextValues[id] = !c.sourceValue(nextValues, id, "in")
		case "SR":
			set := c.sourceValue(nextValues, id, "set")
			reset := c.sourceValue(nextValues, id, "reset")
			latched := c.values[id]
			if reset {
				latched = false
			} else if set {
				latched = true
			}
			nextValues[id] = latched
		case "TON":
			enabled := c.sourceValue(nextValues, id, "in")
			timer := nextTimers[id]
			if enabled {
				timer.ElapsedMs += scanIntervalMs
				if timer.ElapsedMs >= node.PresetMs {
					timer.Q = true
				}
			} else {
				timer = TimerState{}
			}
			nextTimers[id] = timer
			nextValues[id] = timer.Q
		case "OUTPUT":
			nextValues[id] = c.sourceValue(nextValues, id, "in")
		}
	}
	c.values = nextValues
	c.timers = nextTimers
	for id, before := range previousOutputs {
		node := c.program.nodes[id]
		if after := c.values[id]; before != after {
			c.addLog(fmt.Sprintf("输出 %s(%s): %t -> %t", id, node.Label, before, after))
		}
	}
	return c.State()
}

func (c *Controller) sourceValue(values map[string]bool, nodeID, port string) bool {
	ref := c.program.sources[nodeID][port]
	return values[ref.Node]
}

func (c *Controller) outputSnapshot() map[string]bool {
	outputs := map[string]bool{}
	for _, node := range c.program.nodes {
		if node.Kind == "OUTPUT" {
			outputs[node.ID] = c.values[node.ID]
		}
	}
	return outputs
}

func (c *Controller) State() ControllerState {
	outputs := map[string]bool{}
	internals := map[string]any{}
	for _, id := range c.program.order {
		node := c.program.nodes[id]
		if node.Kind == "OUTPUT" {
			outputs[id] = c.values[id]
		} else if node.Kind != "INPUT" {
			internals[id] = c.values[id]
		}
	}
	log := make([]ScanEvent, len(c.log))
	copy(log, c.log)
	if len(log) > 30 {
		log = log[len(log)-30:]
	}
	return ControllerState{
		Mode:        c.mode,
		Version:     c.version,
		ProgramName: c.program.program.Name,
		Program:     c.program.program,
		ScanNumber:  c.scans,
		ScanMs:      scanIntervalMs,
		Inputs:      cloneBools(c.inputs),
		Internals:   internals,
		Outputs:     outputs,
		Timers:      cloneTimers(c.timers),
		Log:         log,
	}
}

func (c *Controller) addLog(message string) {
	c.log = append(c.log, ScanEvent{Scan: c.scans, Message: message})
}

func cloneBools(source map[string]bool) map[string]bool {
	result := make(map[string]bool, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneTimers(source map[string]TimerState) map[string]TimerState {
	result := make(map[string]TimerState, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
