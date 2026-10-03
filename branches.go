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
	original   []ScanRecord
	nextID     int
}

func NewBranchManager(controller *Controller) *BranchManager {
	return &BranchManager{controller: controller, nextID: 1}
}

// SaveBoundary 把当前扫描序号上的输入、内部状态与定时累计保存为边界。
func (m *BranchManager) SaveBoundary(label string) Boundary {
	m.syncOriginalHistory()
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
		Segment:     []map[string]bool{},
		InputPlan:   []InputFrame{},
		Records:     []ScanRecord{},
		Restarts:    []ScanEvent{},
		Status:      "no-result",
	}
	m.branches = append(m.branches, branch)
	m.controller.addLog(fmt.Sprintf("已从边界 %s 建立试验分支 %s（版本 %d，扫描 #%d）",
		boundary.ID, branch.ID, branch.BaseVersion, branch.BaseScan))
	return branch, nil
}

// CreateChildBranch 从父分支已经真实模拟出的扫描记录继续派生。
// 父分支记录之前的共同输出和输入前缀在子分支中只读。
func (m *BranchManager) CreateChildBranch(parentID string, atScan int, name string) (*Branch, error) {
	parent := m.findBranch(parentID)
	if parent == nil {
		return nil, fmt.Errorf("父试验分支 %s 不存在", parentID)
	}
	if parent.Abandoned {
		return nil, fmt.Errorf("父试验分支 %s 已放弃，不能继续派生", parentID)
	}
	if atScan <= parent.BaseScan {
		return nil, fmt.Errorf("子分支起点 %d 必须晚于父分支起点 #%d", atScan, parent.BaseScan)
	}
	record := findRecord(parent.Records, atScan, parent.BaseVersion)
	if record == nil {
		return nil, fmt.Errorf("扫描 #%d 尚无父分支已执行结果，不能从未执行候选片段派生", atScan)
	}
	if _, err := compileProgram(parent.Program); err != nil {
		return nil, fmt.Errorf("父分支程序无法编译：%w", err)
	}
	if name == "" {
		name = fmt.Sprintf("子试验分支（%s @ #%d）", parent.Name, atScan)
	}
	inherited := make([]InputFrame, 0, len(parent.InputPlan))
	for _, frame := range parent.InputPlan {
		if frame.Scan > atScan {
			inherited = append(inherited, cloneInputFrame(frame))
		}
	}
	inheritedSegment := make([]map[string]bool, len(inherited))
	for i := range inherited {
		inheritedSegment[i] = map[string]bool{}
	}
	branch := &Branch{
		ID:           m.allocID("branch"),
		Name:         name,
		BoundaryID:   parent.BoundaryID,
		ParentID:     parent.ID,
		Depth:        parent.Depth + 1,
		BaseScan:     atScan,
		BaseVersion:  parent.BaseVersion,
		InputVersion: 0,
		Status:       "no-result",
		ProgramName:  parent.ProgramName,
		Program:      parent.Program,
		BaseInputs:   cloneBools(record.Inputs),
		BaseValues:   cloneBools(record.Internals),
		BaseTimers:   cloneTimers(record.Timers),
		Segment:      inheritedSegment,
		InputPlan:    inherited,
		Records:      []ScanRecord{},
		Restarts:     []ScanEvent{},
	}
	m.branches = append(m.branches, branch)
	m.controller.addLog(fmt.Sprintf("已从父分支 %s 的扫描 #%d 派生子分支 %s", parent.ID, atScan, branch.ID))
	return branch, nil
}

// ReplaceSegment 替换分支中从 fromScan 起的一段虚拟输入并重新模拟。
// fromScan 之前的分支记录属于共同前缀，只读，不允许被替换覆盖。
func (m *BranchManager) ReplaceSegment(branchID string, fromScan int, inputs []map[string]bool) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if branch.Abandoned {
		return nil, fmt.Errorf("试验分支 %s 已放弃，只能查看保留记录", branchID)
	}
	if fromScan <= branch.BaseScan {
		err := fmt.Errorf("替换起点 %d 必须晚于分支起点扫描 #%d：共同前缀只读", fromScan, branch.BaseScan)
		branch.LastError = err.Error()
		return nil, err
	}
	if len(inputs) == 0 {
		err := errors.New("替换输入段不能为空")
		branch.LastError = err.Error()
		return nil, err
	}
	compiled, err := compileProgram(branch.Program)
	if err != nil {
		branch.LastError = err.Error()
		return nil, err
	}
	for i, row := range inputs {
		for id := range row {
			if _, ok := compiled.inputs[id]; !ok {
				err := fmt.Errorf("第 %d 个扫描的输入 %s 在分支程序中不存在", fromScan+i, id)
				branch.LastError = err.Error()
				return nil, err
			}
		}
	}
	endScan := fromScan + len(inputs) - 1
	currentEnd := branch.BaseScan + len(branch.InputPlan)
	if fromScan > currentEnd+1 {
		err := fmt.Errorf("替换起点 #%d 超出连续范围（当前已执行到 #%d）", fromScan, currentEnd)
		branch.LastError = err.Error()
		return nil, err
	}
	overlapsParent := fromScan <= branch.BaseScan+len(branch.InputPlan)
	oldSegment := cloneSegment(branch.Segment)
	oldPlan := cloneInputFrames(branch.InputPlan)
	oldRecords := cloneRecords(branch.Records)
	oldDivergence := branch.Divergence
	oldVersion := branch.InputVersion
	prefix := fromScan - branch.BaseScan - 1
	if prefix < 0 {
		return nil, fmt.Errorf("替换起点 %d 必须晚于分支起点扫描 #%d：共同前缀只读", fromScan, branch.BaseScan)
	}
	nextSegment := cloneSegment(branch.Segment[:min(prefix, len(branch.Segment))])
	for _, row := range inputs {
		nextSegment = append(nextSegment, cloneBools(row))
	}
	branch.Segment = nextSegment
	branch.InputVersion++
	if err := m.simulate(branch); err != nil {
		branch.Segment = oldSegment
		branch.InputPlan = oldPlan
		branch.Records = oldRecords
		branch.Divergence = oldDivergence
		branch.InputVersion = oldVersion
		branch.LastError = err.Error()
		return nil, err
	}
	branch.LastError = ""
	branch.Status = "effective"
	overlapNote := ""
	if overlapsParent {
		overlapNote = fmt.Sprintf("，重叠范围 #%d..#%d 以本次输入版本 %d 为准", fromScan, min(endScan, currentEnd), branch.InputVersion)
	}
	m.controller.addLog(fmt.Sprintf("分支 %s 已生效输入版本 %d：替换 #%d..#%d%s", branch.ID, branch.InputVersion, fromScan, endScan, overlapNote))
	return branch, nil
}

func (m *BranchManager) AbandonBranch(branchID string) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	branch.Abandoned = true
	m.controller.addLog(fmt.Sprintf("试验分支 %s 已放弃，记录保留且不再接回原执行", branch.ID))
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
	if branch.Abandoned {
		return nil, fmt.Errorf("试验分支 %s 已放弃，不能重置元件", branchID)
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
	oldPlan := cloneInputFrames(branch.InputPlan)
	oldRecords := cloneRecords(branch.Records)
	oldDivergence := branch.Divergence
	if err := m.simulate(branch); err != nil {
		branch.InputPlan = oldPlan
		branch.Records = oldRecords
		branch.Divergence = oldDivergence
		branch.LastError = err.Error()
		return nil, err
	}
	branch.LastError = ""
	branch.Status = "effective"
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
	m.syncOriginalHistory()
	plan, err := m.buildInputPlan(branch, compiled)
	if err != nil {
		return err
	}
	inputs := cloneBools(branch.BaseInputs)
	values := cloneBools(branch.BaseValues)
	timers := cloneTimers(branch.BaseTimers)
	records := make([]ScanRecord, 0, len(plan))
	for _, frame := range plan {
		for id, value := range frame.Inputs {
			inputs[id] = value
		}
		values, timers = scanOnce(compiled, inputs, values, timers)
		outputs, internals := splitValues(compiled, values)
		records = append(records, ScanRecord{
			Scan:      frame.Scan,
			Version:   branch.BaseVersion,
			Inputs:    cloneBools(inputs),
			Outputs:   outputs,
			Internals: internals,
			Timers:    cloneTimers(timers),
		})
	}
	branch.InputPlan = plan
	branch.Records = records
	branch.Divergence = m.divergence(branch)
	branch.Status = "effective"
	return nil
}

func (m *BranchManager) buildInputPlan(branch *Branch, compiled *CompiledProgram) ([]InputFrame, error) {
	parent := m.findBranch(branch.ParentID)
	parentOffset := 0
	if parent != nil {
		parentOffset = branch.BaseScan - parent.BaseScan
	}
	length := len(branch.Segment)
	plan := make([]InputFrame, 0, length)
	lastInputs := cloneBools(branch.BaseInputs)
	lastSources := map[string]string{}
	for id := range compiled.inputs {
		lastSources[id] = "扫描边界输入"
	}
	for offset := range length {
		scan := branch.BaseScan + 1 + offset
		effective := cloneBools(lastInputs)
		sources := cloneSources(lastSources)
		sourceBranch := branch.ParentID
		sourceName := "父分支继承输入"
		sourceVersion := 0
		parentFrameOffset := parentOffset + offset
		if parent != nil && parentFrameOffset < len(parent.InputPlan) {
			parentFrame := parent.InputPlan[parentFrameOffset]
			effective = cloneBools(parentFrame.Inputs)
			sources = cloneSources(parentFrame.Sources)
			sourceBranch = parentFrame.BranchID
			sourceName = parentFrame.BranchName
			sourceVersion = parentFrame.InputVersion
		}
		if offset < len(branch.Segment) {
			sourceBranch = branch.ID
			sourceName = branch.Name
			sourceVersion = branch.InputVersion
			for id, value := range branch.Segment[offset] {
				if _, ok := compiled.inputs[id]; !ok {
					return nil, fmt.Errorf("扫描 #%d 的输入 %s 在分支程序中不存在", scan, id)
				}
				effective[id] = value
				sources[id] = fmt.Sprintf("%s 输入版本 %d", branch.Name, branch.InputVersion)
			}
		}
		if len(sources) == 0 {
			for id := range compiled.inputs {
				sources[id] = "扫描边界输入"
			}
		}
		lastInputs = cloneBools(effective)
		lastSources = cloneSources(sources)
		plan = append(plan, InputFrame{
			Scan: scan, Inputs: effective, Sources: sources,
			BranchID: sourceBranch, BranchName: sourceName, InputVersion: sourceVersion,
		})
	}
	return plan, nil
}

// divergence 找到分支输出与原执行历史（同版本、同扫描序号）首次
// 不一致的位置，并列出该位置涉及的内部状态。
func (m *BranchManager) divergence(branch *Branch) *Divergence {
	var baseline []ScanRecord
	baselineID := "original"
	baselineName := "原执行"
	if parent := m.findBranch(branch.ParentID); parent != nil {
		baseline = parent.Records
		baselineID = parent.ID
		baselineName = parent.Name
	} else {
		baseline = m.original
	}
	for _, record := range branch.Records {
		base := findRecord(baseline, record.Scan, branch.BaseVersion)
		if base == nil {
			continue
		}
		outputs := diffKeys(record.Outputs, base.Outputs)
		if len(outputs) == 0 {
			continue
		}
		internals := diffKeys(record.Internals, base.Internals)
		timers := diffTimers(record.Timers, base.Timers)
		states := internalStatesAt(record, outputs, internals, timers)
		return &Divergence{
			Scan: record.Scan, BaselineID: baselineID, BaselineName: baselineName,
			Outputs: outputs, Internals: internals, Timers: timers,
			InternalStates: states,
			Message: fmt.Sprintf("扫描 #%d 输出首次出现差异：%s；涉及内部状态：%s",
				record.Scan, joinOrNone(outputs), joinOrNone(append(append(append([]string{}, internals...), timers...), outputs...))),
		}
	}
	return nil
}

func internalStatesAt(record ScanRecord, ids ...[]string) map[string]InternalState {
	result := map[string]InternalState{}
	for _, group := range ids {
		for _, id := range group {
			state := InternalState{}
			if value, ok := record.Internals[id]; ok {
				state.Value = value
			}
			if timer, ok := record.Timers[id]; ok {
				state.ElapsedMs = timer.ElapsedMs
				state.Q = timer.Q
				state.Value = timer.Q
			}
			result[id] = state
		}
	}
	return result
}

func diffTimers(a, b map[string]TimerState) []string {
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
	NextID     int          `json:"nextId"`
	Boundaries []Boundary   `json:"boundaries"`
	Branches   []*Branch    `json:"branches"`
	Original   []ScanRecord `json:"original"`
}

// Save 把边界与分支（含替换输入段、程序版本、扫描序号）持久化到磁盘。
func (m *BranchManager) Save(path string) error {
	store := branchStore{NextID: m.nextID, Boundaries: m.boundaries, Branches: m.branches, Original: m.original}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
	for _, branch := range store.Branches {
		if branch == nil {
			return errors.New("分支存储包含空分支")
		}
		if _, err := compileProgram(branch.Program); err != nil {
			return fmt.Errorf("分支 %s 程序无法编译：%w", branch.ID, err)
		}
		if branch.Segment == nil {
			branch.Segment = []map[string]bool{}
		}
		if branch.InputPlan == nil {
			branch.InputPlan = []InputFrame{}
		}
		if branch.Records == nil {
			branch.Records = []ScanRecord{}
		}
		if branch.Restarts == nil {
			branch.Restarts = []ScanEvent{}
		}
		if branch.Status == "" {
			if len(branch.Records) > 0 {
				branch.Status = "effective"
			} else {
				branch.Status = "no-result"
			}
		}
	}
	m.nextID = max(store.NextID, 1)
	m.boundaries = store.Boundaries
	m.branches = store.Branches
	m.original = store.Original
	return nil
}

func (m *BranchManager) syncOriginalHistory() {
	existing := map[[2]int]bool{}
	for _, record := range m.original {
		existing[[2]int{record.Scan, record.Version}] = true
	}
	for _, record := range m.controller.history {
		key := [2]int{record.Scan, record.Version}
		if !existing[key] {
			m.original = append(m.original, cloneRecord(record))
			existing[key] = true
		}
	}
}

func cloneRecord(source ScanRecord) ScanRecord {
	return ScanRecord{
		Scan: source.Scan, Version: source.Version,
		Inputs: cloneBools(source.Inputs), Outputs: cloneBools(source.Outputs),
		Internals: cloneBools(source.Internals), Timers: cloneTimers(source.Timers),
	}
}

func cloneRecords(source []ScanRecord) []ScanRecord {
	result := make([]ScanRecord, len(source))
	for i := range source {
		result[i] = cloneRecord(source[i])
	}
	return result
}

func cloneSegment(source []map[string]bool) []map[string]bool {
	result := make([]map[string]bool, len(source))
	for i, row := range source {
		result[i] = cloneBools(row)
	}
	return result
}

func cloneSources(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneInputFrame(frame InputFrame) InputFrame {
	return InputFrame{
		Scan: frame.Scan, Inputs: cloneBools(frame.Inputs), Sources: cloneSources(frame.Sources),
		BranchID: frame.BranchID, BranchName: frame.BranchName, InputVersion: frame.InputVersion,
	}
}

func cloneInputFrames(frames []InputFrame) []InputFrame {
	result := make([]InputFrame, len(frames))
	for i := range frames {
		result[i] = cloneInputFrame(frames[i])
	}
	return result
}
