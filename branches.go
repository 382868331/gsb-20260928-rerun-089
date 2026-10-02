package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
)

// BranchManager 管理扫描边界与输入试验分支。边界是只读快照，
// 分支从边界分叉并用替换输入段重新模拟，绝不回写控制器已发生的状态。
type BranchManager struct {
	controller *Controller
	boundaries []Boundary
	branches   []*Branch
	nextID     int
}

func NewBranchManager(controller *Controller) *BranchManager {
	return &BranchManager{controller: controller, nextID: 1}
}

// SaveBoundary 把当前扫描序号上的输入、内部状态与定时累计保存为边界。
func (m *BranchManager) SaveBoundary(label string) Boundary {
	if label == "" {
		label = fmt.Sprintf("扫描 #%d 边界", m.controller.scans)
	}
	boundary := Boundary{
		ID:      m.allocID("boundary"),
		Label:   label,
		Scan:    m.controller.scans,
		Version: m.controller.version,
		Program: m.controller.program.program,
		Inputs:  cloneBools(m.controller.inputs),
		Values:  cloneBools(m.controller.values),
		Timers:  cloneTimers(m.controller.timers),
	}
	m.boundaries = append(m.boundaries, boundary)
	m.controller.addLog(fmt.Sprintf("已保存扫描边界 %s（扫描 #%d，版本 %d）", boundary.ID, boundary.Scan, boundary.Version))
	return boundary
}

// CreateBranch 从已保存的边界建立试验分支。分支冻结边界当时的程序
// 版本与状态，之后主程序修订（包括元件删除）不影响分支的共同前缀。
func (m *BranchManager) CreateBranch(boundaryID, name string) (*Branch, error) {
	boundary := m.findBoundary(boundaryID)
	if boundary == nil {
		return nil, fmt.Errorf("扫描边界 %s 不存在", boundaryID)
	}
	if _, err := compileProgram(boundary.Program); err != nil {
		return nil, fmt.Errorf("边界程序无法编译：%w", err)
	}
	if name == "" {
		name = fmt.Sprintf("试验分支（基于扫描 #%d）", boundary.Scan)
	}
	branch := &Branch{
		ID:          m.allocID("branch"),
		Name:        name,
		BoundaryID:  boundary.ID,
		BaseScan:    boundary.Scan,
		BaseVersion: boundary.Version,
		ProgramName: boundary.Program.Name,
		Program:     boundary.Program,
		BaseInputs:  cloneBools(boundary.Inputs),
		BaseValues:  cloneBools(boundary.Values),
		BaseTimers:  cloneTimers(boundary.Timers),
	}
	m.branches = append(m.branches, branch)
	m.controller.addLog(fmt.Sprintf("已从边界 %s 建立试验分支 %s（版本 %d，扫描 #%d）",
		boundary.ID, branch.ID, branch.BaseVersion, branch.BaseScan))
	return branch, nil
}

// ReplaceSegment 替换分支中从 fromScan 起的一段虚拟输入并重新模拟。
// fromScan 之前的分支记录属于共同前缀，只读，不允许被替换覆盖。
func (m *BranchManager) ReplaceSegment(branchID string, fromScan int, inputs []map[string]bool) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if fromScan <= branch.BaseScan {
		return nil, fmt.Errorf("替换起点 %d 必须晚于分支起点扫描 #%d：共同前缀只读", fromScan, branch.BaseScan)
	}
	if len(inputs) == 0 {
		return nil, errors.New("替换输入段不能为空")
	}
	compiled, err := compileProgram(branch.Program)
	if err != nil {
		return nil, err
	}
	for i, row := range inputs {
		for id := range row {
			if _, ok := compiled.inputs[id]; !ok {
				return nil, fmt.Errorf("第 %d 个扫描的输入 %s 在分支程序中不存在", fromScan+i, id)
			}
		}
	}
	prefix := fromScan - branch.BaseScan - 1
	if prefix > len(branch.Segment) {
		return nil, fmt.Errorf("替换起点 %d 超出了已模拟范围（当前到扫描 #%d）", fromScan, branch.BaseScan+len(branch.Segment))
	}
	branch.Segment = append(append([]map[string]bool{}, branch.Segment[:prefix]...), inputs...)
	if err := m.simulate(branch); err != nil {
		return nil, err
	}
	m.controller.addLog(fmt.Sprintf("分支 %s 已替换自扫描 #%d 起的 %d 个扫描输入", branch.ID, fromScan, len(inputs)))
	return branch, nil
}

// RestartElement 在分支起点将某个元件的状态明确清零（定时器累计或
// 保持值），并留下记录。跨分支起点的定时器默认保留累计依据，只有
// 用户显式重新开始才清零。
func (m *BranchManager) RestartElement(branchID, nodeID string) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	var node *Node
	for i := range branch.Program.Nodes {
		if branch.Program.Nodes[i].ID == nodeID {
			node = &branch.Program.Nodes[i]
			break
		}
	}
	if node == nil {
		return nil, fmt.Errorf("元件 %s 不在分支程序中", nodeID)
	}
	switch node.Kind {
	case "TON":
		branch.BaseTimers[nodeID] = TimerState{}
	case "SR", "AND", "OR", "NOT":
		branch.BaseValues[nodeID] = false
	default:
		return nil, fmt.Errorf("元件 %s(%s) 没有可重新开始的内部状态", nodeID, node.Label)
	}
	branch.Restarts = append(branch.Restarts, ScanEvent{
		Scan:    branch.BaseScan,
		Message: fmt.Sprintf("用户在分支起点明确重新开始元件 %s(%s)，其状态已清零", nodeID, node.Label),
	})
	if err := m.simulate(branch); err != nil {
		return nil, err
	}
	m.controller.addLog(fmt.Sprintf("分支 %s 的元件 %s 已在起点重新开始", branch.ID, nodeID))
	return branch, nil
}

// simulate 从边界快照出发，按替换输入段逐扫描重放，得到分支记录，
// 并与原执行历史比较首次输出差异。定时器以边界累计为依据继续计时。
func (m *BranchManager) simulate(branch *Branch) error {
	compiled, err := compileProgram(branch.Program)
	if err != nil {
		return err
	}
	inputs := cloneBools(branch.BaseInputs)
	values := cloneBools(branch.BaseValues)
	timers := cloneTimers(branch.BaseTimers)
	records := make([]ScanRecord, 0, len(branch.Segment))
	for i, override := range branch.Segment {
		scan := branch.BaseScan + 1 + i
		for id, value := range override {
			inputs[id] = value
		}
		values, timers = scanOnce(compiled, inputs, values, timers)
		outputs, internals := splitValues(compiled, values)
		records = append(records, ScanRecord{
			Scan:      scan,
			Version:   branch.BaseVersion,
			Inputs:    cloneBools(inputs),
			Outputs:   outputs,
			Internals: internals,
			Timers:    cloneTimers(timers),
		})
	}
	branch.Records = records
	branch.Divergence = m.divergence(branch)
	return nil
}

// divergence 找到分支输出与原执行历史（同版本、同扫描序号）首次
// 不一致的位置，并列出该位置涉及的内部状态。
func (m *BranchManager) divergence(branch *Branch) *Divergence {
	for _, record := range branch.Records {
		base := findRecord(m.controller.history, record.Scan, branch.BaseVersion)
		if base == nil {
			continue
		}
		outputs := diffKeys(record.Outputs, base.Outputs)
		if len(outputs) == 0 {
			continue
		}
		internals := diffKeys(record.Internals, base.Internals)
		return &Divergence{
			Scan:      record.Scan,
			Outputs:   outputs,
			Internals: internals,
			Message: fmt.Sprintf("扫描 #%d 输出首次出现差异：%s；涉及内部状态：%s",
				record.Scan, joinOrNone(outputs), joinOrNone(internals)),
		}
	}
	return nil
}

func findRecord(history []ScanRecord, scan, version int) *ScanRecord {
	for i := range history {
		if history[i].Scan == scan && history[i].Version == version {
			return &history[i]
		}
	}
	return nil
}

func diffKeys(a, b map[string]bool) []string {
	keys := map[string]bool{}
	for id := range a {
		keys[id] = true
	}
	for id := range b {
		keys[id] = true
	}
	var diff []string
	for id := range keys {
		if a[id] != b[id] {
			diff = append(diff, id)
		}
	}
	sort.Strings(diff)
	return diff
}

func joinOrNone(ids []string) string {
	if len(ids) == 0 {
		return "无"
	}
	result := ids[0]
	for _, id := range ids[1:] {
		result += ", " + id
	}
	return result
}

func (m *BranchManager) findBoundary(id string) *Boundary {
	for i := range m.boundaries {
		if m.boundaries[i].ID == id {
			return &m.boundaries[i]
		}
	}
	return nil
}

func (m *BranchManager) findBranch(id string) *Branch {
	for _, branch := range m.branches {
		if branch.ID == id {
			return branch
		}
	}
	return nil
}

func (m *BranchManager) allocID(prefix string) string {
	id := fmt.Sprintf("%s-%d", prefix, m.nextID)
	m.nextID++
	return id
}

// Boundaries 返回边界列表副本，Branches 返回分支列表（值）副本。
func (m *BranchManager) Boundaries() []Boundary {
	result := make([]Boundary, len(m.boundaries))
	copy(result, m.boundaries)
	return result
}

func (m *BranchManager) Branches() []Branch {
	result := make([]Branch, 0, len(m.branches))
	for _, branch := range m.branches {
		result = append(result, *branch)
	}
	return result
}

type branchStore struct {
	NextID     int        `json:"nextId"`
	Boundaries []Boundary `json:"boundaries"`
	Branches   []*Branch  `json:"branches"`
}

// Save 把边界与分支（含替换输入段、程序版本、扫描序号）持久化到磁盘。
func (m *BranchManager) Save(path string) error {
	store := branchStore{NextID: m.nextID, Boundaries: m.boundaries, Branches: m.branches}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Load 从磁盘恢复边界与分支；文件不存在时视为空存储。
func (m *BranchManager) Load(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var store branchStore
	if err := json.Unmarshal(data, &store); err != nil {
		return err
	}
	m.nextID = store.NextID
	m.boundaries = store.Boundaries
	m.branches = store.Branches
	if m.nextID < 1 {
		m.nextID = 1
	}
	return nil
}
