package main

import (
	"errors"
	"fmt"
	"sort"
)

// SaveBoundary 把当前扫描序号上的完整状态保存为只读扫描边界，
// 作为后续试验分支的共同前缀起点。
func (c *Controller) SaveBoundary(name string) Boundary {
	c.nextBoundaryID++
	boundary := &Boundary{
		ID:      fmt.Sprintf("boundary-%d", c.nextBoundaryID),
		Name:    name,
		Scan:    c.scans,
		Version: c.version,
		Program: c.program.program,
		Inputs:  cloneBools(c.inputs),
		Values:  cloneBools(c.values),
		Timers:  cloneTimers(c.timers),
	}
	if boundary.Name == "" {
		boundary.Name = fmt.Sprintf("边界 #%d", boundary.Scan)
	}
	c.boundaries = append(c.boundaries, boundary)
	c.addLog(fmt.Sprintf("已保存扫描边界「%s」（扫描 %d，版本 %d）", boundary.Name, boundary.Scan, boundary.Version))
	return *boundary
}

func (c *Controller) findBoundary(id string) *Boundary {
	for _, boundary := range c.boundaries {
		if boundary.ID == id {
			return boundary
		}
	}
	return nil
}

func (c *Controller) findBranch(id string) *Branch {
	for _, branch := range c.branches {
		if branch.ID == id {
			return branch
		}
	}
	return nil
}

// CreateBranch 从已保存的扫描边界建立输入试验分支。分支复制边界的
// 输入采样、内部状态与定时器累计（跨起点的定时器保留累计依据），
// 并固定使用边界保存时的程序版本；原执行的状态与输出不受影响。
func (c *Controller) CreateBranch(boundaryID, name string) (*Branch, error) {
	boundary := c.findBoundary(boundaryID)
	if boundary == nil {
		return nil, fmt.Errorf("扫描边界 %s 不存在", boundaryID)
	}
	c.nextBranchID++
	branch := &Branch{
		ID:         fmt.Sprintf("branch-%d", c.nextBranchID),
		Name:       name,
		BoundaryID: boundary.ID,
		OriginScan: boundary.Scan,
		Version:    boundary.Version,
		Program:    boundary.Program,
		Inputs:     cloneBools(boundary.Inputs),
		Values:     cloneBools(boundary.Values),
		Timers:     cloneTimers(boundary.Timers),
		Scan:       boundary.Scan,
	}
	if branch.Name == "" {
		branch.Name = fmt.Sprintf("分支 @#%d", boundary.Scan)
	}
	c.branches = append(c.branches, branch)
	c.addLog(fmt.Sprintf("已从边界「%s」建立试验分支「%s」（起点扫描 %d，程序版本 %d）",
		boundary.Name, branch.Name, branch.OriginScan, branch.Version))
	return branch, nil
}

// SetBranchOverrides 整体替换分支的输入覆盖表。覆盖只允许作用于分支
// 起点之后的扫描（共同前缀只读），且不能改写分支已经执行过的扫描。
func (c *Controller) SetBranchOverrides(branchID string, overrides []InputOverride) error {
	branch := c.findBranch(branchID)
	if branch == nil {
		return fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	compiled, err := compileProgram(branch.Program)
	if err != nil {
		return err
	}
	seen := map[int]string{}
	for _, override := range overrides {
		if override.Scan <= branch.OriginScan {
			return fmt.Errorf("扫描 %d 属于共同前缀（起点 #%d），共同前缀只读，不能覆盖", override.Scan, branch.OriginScan)
		}
		if override.Scan <= branch.Scan {
			return fmt.Errorf("扫描 %d 已在分支中执行（当前 #%d），不能改写已发生的扫描", override.Scan, branch.Scan)
		}
		if _, ok := compiled.inputs[override.Input]; !ok {
			return fmt.Errorf("分支程序中不存在输入 %s", override.Input)
		}
		if previous, dup := seen[override.Scan]; dup {
			return fmt.Errorf("扫描 %d 的输入 %s 与 %s 覆盖冲突", override.Scan, previous, override.Input)
		}
		seen[override.Scan] = override.Input
	}
	branch.Overrides = append([]InputOverride(nil), overrides...)
	return nil
}

// ScanBranch 推进分支扫描：每个扫描先应用该序号的输入覆盖，
// 再按边界保存的程序版本求值，并记录输出与内部状态用于比较。
func (c *Controller) ScanBranch(branchID string, count int) (*Branch, error) {
	branch := c.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if count < 1 || count > 100 {
		return nil, errors.New("count 必须在 1 到 100 之间")
	}
	compiled, err := compileProgram(branch.Program)
	if err != nil {
		return nil, err
	}
	overridesByScan := map[int][]InputOverride{}
	for _, override := range branch.Overrides {
		overridesByScan[override.Scan] = append(overridesByScan[override.Scan], override)
	}
	for range count {
		branch.Scan++
		for _, override := range overridesByScan[branch.Scan] {
			branch.Inputs[override.Input] = override.Value
		}
		branch.Values, branch.Timers = evalScan(compiled, branch.Inputs, branch.Values, branch.Timers)
		branch.Records = append(branch.Records, ScanRecord{
			Scan:      branch.Scan,
			Outputs:   branch.outputSnapshot(),
			Internals: branch.internalSnapshot(),
		})
	}
	return branch, nil
}

// CompareBranch 逐扫描比较分支与原执行的输出，返回首次出现差异的
// 扫描序号、差异输出以及该扫描上涉及的内部状态。比较为只读操作。
func (c *Controller) CompareBranch(branchID string) (*Divergence, error) {
	branch := c.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	mainByScan := map[int]ScanRecord{}
	for _, record := range c.history {
		mainByScan[record.Scan] = record
	}
	labels := map[string]string{}
	for _, node := range branch.Program.Nodes {
		labels[node.ID] = node.Label
	}
	divergence := &Divergence{}
	for _, record := range branch.Records {
		main, ok := mainByScan[record.Scan]
		if !ok {
			continue
		}
		divergence.ComparedScans++
		outputs := diffKeys(main.Outputs, record.Outputs)
		if len(outputs) == 0 {
			continue
		}
		divergence.Found = true
		divergence.Scan = record.Scan
		for _, id := range outputs {
			divergence.Outputs = append(divergence.Outputs,
				fmt.Sprintf("输出 %s(%s): 原执行=%t 分支=%t", id, labels[id], main.Outputs[id], record.Outputs[id]))
		}
		for _, id := range diffKeys(main.Internals, record.Internals) {
			divergence.Internals = append(divergence.Internals,
				fmt.Sprintf("内部状态 %s(%s): 原执行=%t 分支=%t", id, labels[id], main.Internals[id], record.Internals[id]))
		}
		return divergence, nil
	}
	return divergence, nil
}

func diffKeys(main, branch map[string]bool) []string {
	keys := map[string]bool{}
	for id := range main {
		keys[id] = true
	}
	for id := range branch {
		keys[id] = true
	}
	diffs := []string{}
	for id := range keys {
		if main[id] != branch[id] {
			diffs = append(diffs, id)
		}
	}
	sort.Strings(diffs)
	return diffs
}

// ResetBranchElement 在分支内重新开始某个保持类元件（TON 清零累计、
// SR 清除保持），并留下带扫描序号的明确记录。
func (c *Controller) ResetBranchElement(branchID, nodeID string) error {
	branch := c.findBranch(branchID)
	if branch == nil {
		return fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	var target *Node
	for i := range branch.Program.Nodes {
		if branch.Program.Nodes[i].ID == nodeID {
			target = &branch.Program.Nodes[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("分支程序中不存在元件 %s", nodeID)
	}
	switch target.Kind {
	case "TON":
		branch.Timers[nodeID] = TimerState{}
		branch.Values[nodeID] = false
	case "SR":
		branch.Values[nodeID] = false
	default:
		return fmt.Errorf("元件 %s 类型为 %s，仅支持重新开始 TON/SR 保持类元件", nodeID, target.Kind)
	}
	branch.ResetLog = append(branch.ResetLog, ScanEvent{
		Scan:    branch.Scan,
		Message: fmt.Sprintf("用户在扫描 #%d 重新开始元件 %s(%s)，其累计/保持已清零", branch.Scan, target.ID, target.Label),
	})
	return nil
}

func (b *Branch) outputSnapshot() map[string]bool {
	outputs := map[string]bool{}
	for _, node := range b.Program.Nodes {
		if node.Kind == "OUTPUT" {
			outputs[node.ID] = b.Values[node.ID]
		}
	}
	return outputs
}

func (b *Branch) internalSnapshot() map[string]bool {
	internals := map[string]bool{}
	for _, node := range b.Program.Nodes {
		if node.Kind != "INPUT" && node.Kind != "OUTPUT" {
			internals[node.ID] = b.Values[node.ID]
		}
	}
	return internals
}

func (b *Branch) view() BranchView {
	resetLog := make([]ScanEvent, len(b.ResetLog))
	copy(resetLog, b.ResetLog)
	overrides := make([]InputOverride, len(b.Overrides))
	copy(overrides, b.Overrides)
	return BranchView{
		ID:         b.ID,
		Name:       b.Name,
		BoundaryID: b.BoundaryID,
		OriginScan: b.OriginScan,
		Version:    b.Version,
		Scan:       b.Scan,
		Program:    b.Program,
		Inputs:     cloneBools(b.Inputs),
		Overrides:  overrides,
		Outputs:    b.outputSnapshot(),
		Timers:     cloneTimers(b.Timers),
		ResetLog:   resetLog,
	}
}
