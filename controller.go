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

func (c *Controller) Load(program Program) error {
	compiled, err := compileProgram(program)
	if err != nil {
		return err
	}
	c.program = compiled
	c.version++
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
	c.addLog("编辑后的程序已作为新版本载入")
	return nil
}

// Preview 校验待载入程序并报告相对当前版本的修订影响，
// 不修改任何运行状态；非法程序在生效前被拒绝。
func (c *Controller) Preview(program Program) (*ProgramDiff, error) {
	compiled, err := compileProgram(program)
	if err != nil {
		return nil, err
	}
	diff := &ProgramDiff{Compatible: true, Paused: c.mode == "paused"}
	for id, oldNode := range c.program.nodes {
		newNode, exists := compiled.nodes[id]
		if !exists {
			note := fmt.Sprintf("元件 %s(%s) 已删除", id, oldNode.Label)
			if oldNode.Kind == "TON" && c.timers[id].ElapsedMs > 0 {
				note += fmt.Sprintf("，其累计 %dms 将丢弃", c.timers[id].ElapsedMs)
			}
			if oldNode.Kind == "SR" && c.values[id] {
				note += "，其保持状态将丢弃"
			}
			diff.RemovedNodes = append(diff.RemovedNodes, note)
			diff.Compatible = false
			continue
		}
		if oldNode.Kind == "INPUT" && oldNode.Label != newNode.Label {
			diff.RenamedInputs = append(diff.RenamedInputs,
				fmt.Sprintf("输入 %s 由「%s」重命名为「%s」，采样值保留", id, oldNode.Label, newNode.Label))
		}
		if oldNode.Kind == "TON" && oldNode.PresetMs != newNode.PresetMs {
			diff.TimerChanges = append(diff.TimerChanges,
				fmt.Sprintf("互锁延时 %s(%s) 预设 %dms -> %dms，该定时器累计需清零", id, newNode.Label, oldNode.PresetMs, newNode.PresetMs))
			diff.Compatible = false
		}
		if oldNode.Kind != newNode.Kind {
			diff.Messages = append(diff.Messages,
				fmt.Sprintf("元件 %s 类型由 %s 变为 %s，状态无法保留", id, oldNode.Kind, newNode.Kind))
			diff.Compatible = false
		}
	}
	for id, newNode := range compiled.nodes {
		if _, exists := c.program.nodes[id]; !exists {
			diff.AddedNodes = append(diff.AddedNodes, fmt.Sprintf("新增元件 %s(%s)", id, newNode.Label))
		}
	}
	if len(diff.TimerChanges) == 0 && len(diff.RemovedNodes) == 0 && len(diff.RenamedInputs) == 0 && len(diff.AddedNodes) == 0 && len(diff.Messages) == 0 {
		diff.Messages = append(diff.Messages, "程序逻辑无实质变化")
	}
	return diff, nil
}

// LoadPreserve 将编辑后的程序作为新版本载入，同时保留与新程序兼容的
// 已暂停/运行状态：存续输入的采样值、未改预设的定时器累计、存续 SR 的
// 保持值与扫描计数。暂停时载入后仍处于暂停，不会默默清零后继续计时。
func (c *Controller) LoadPreserve(program Program) error {
	diff, err := c.Preview(program)
	if err != nil {
		return err
	}
	if !diff.Compatible {
		return errors.New("新版本包含不兼容变更（元件删除或互锁延时修改），必须明确重新开始")
	}
	compiled, _ := compileProgram(program)
	c.program = compiled
	c.version++
	nextInputs := map[string]bool{}
	for id := range compiled.inputs {
		nextInputs[id] = c.inputs[id]
	}
	c.inputs = nextInputs
	nextValues := map[string]bool{}
	nextTimers := map[string]TimerState{}
	for id, node := range compiled.nodes {
		if node.Kind == "TON" {
			nextTimers[id] = c.timers[id]
		}
		if node.Kind != "INPUT" && node.Kind != "OUTPUT" {
			nextValues[id] = c.values[id]
		}
	}
	c.values = nextValues
	c.timers = nextTimers
	c.addLog(fmt.Sprintf("新版本以保留兼容状态方式载入（模式 %s，扫描计数 %d 保持不变）", c.mode, c.scans))
	return nil
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
