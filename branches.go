package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
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

const (
	// branchStatusCreated：分支已创建，但尚无任何模拟结果（尚无结果）。
	branchStatusCreated = "created"
	// branchStatusApplied：已执行替换输入段并产生可比较的模拟结果（已经生效）。
	branchStatusApplied = "applied"
	// branchStatusAbandoned：用户放弃的中间分支，只读保留，不再可操作。
	branchStatusAbandoned = "abandoned"
)

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
		ID:           m.allocID("branch"),
		Name:         name,
		BoundaryID:   boundary.ID,
		BaseScan:     boundary.Scan,
		BaseVersion:  boundary.Version,
		InputVersion: 0,
		Status:       branchStatusCreated,
		ProgramName:  boundary.Program.Name,
		Program:      boundary.Program,
		BaseInputs:   cloneBools(boundary.Inputs),
		BaseValues:   cloneBools(boundary.Values),
		BaseTimers:   cloneTimers(boundary.Timers),
		Segment:      []map[string]bool{},
		Origins:      []RowOrigin{},
		Records:      []ScanRecord{},
		Restarts:     []ScanEvent{},
		OverlapNotes: []string{},
		Reports:      []CompareReport{},
	}
	m.branches = append(m.branches, branch)
	m.controller.addLog(fmt.Sprintf("已从边界 %s 建立试验分支 %s（版本 %d，扫描 #%d）",
		boundary.ID, branch.ID, branch.BaseVersion, branch.BaseScan))
	return branch, nil
}

// ForkBranch 在一个试验分支上继续派生子分支。父分支在 forkScan 及之前
// 已发生的输出属于只读共同前缀；forkScan 之后父分支的候选输入默认作为
// “父分支模拟输入”延续（明确标注来源，不算真实扫描），子分支可以对
// 不同时间段做多轮替换。
func (m *BranchManager) ForkBranch(parentID, name string, forkScan int) (*Branch, error) {
	parent := m.findBranch(parentID)
	if parent == nil {
		return nil, fmt.Errorf("父分支 %s 不存在", parentID)
	}
	if parent.Status == branchStatusAbandoned {
		return nil, fmt.Errorf("父分支 %s 已放弃，不能再派生", parentID)
	}
	if len(parent.Records) == 0 {
		return nil, errors.New("父分支尚无模拟结果，请先在父分支替换输入段后再派生")
	}
	if forkScan <= parent.BaseScan {
		return nil, fmt.Errorf("派生点 %d 必须晚于父分支起点扫描 #%d：共同前缀只读", forkScan, parent.BaseScan)
	}
	forkRecord := findRecord(parent.Records, forkScan, parent.BaseVersion)
	if forkRecord == nil {
		return nil, fmt.Errorf("派生点扫描 #%d 超出父分支已模拟范围（当前到扫描 #%d）", forkScan, parent.BaseScan+len(parent.Records))
	}
	if _, err := compileProgram(parent.Program); err != nil {
		return nil, fmt.Errorf("父分支程序无法编译：%w", err)
	}
	if name == "" {
		name = fmt.Sprintf("子分支（%s @ 扫描 #%d）", parent.Name, forkScan)
	}
	values := cloneBools(forkRecord.Internals)
	for id, value := range forkRecord.Outputs {
		values[id] = value
	}
	child := &Branch{
		ID:           m.allocID("branch"),
		Name:         name,
		BoundaryID:   parent.BoundaryID,
		ParentID:     parent.ID,
		ParentName:   parent.Name,
		ForkScan:     forkScan,
		BaseScan:     forkScan,
		BaseVersion:  parent.BaseVersion,
		InputVersion: 0,
		Status:       branchStatusCreated,
		ProgramName:  parent.ProgramName,
		Program:      parent.Program,
		BaseInputs:   cloneBools(forkRecord.Inputs),
		BaseValues:   values,
		BaseTimers:   cloneTimers(forkRecord.Timers),
		Segment:      []map[string]bool{},
		Origins:      []RowOrigin{},
		Records:      []ScanRecord{},
		Restarts:     []ScanEvent{},
		OverlapNotes: []string{},
		// 对照依据固定为派生时父分支的输入版本与已模拟记录快照；
		// 父分支之后替换输入不会悄悄改变子分支的共同前缀与比较依据。
		BaseInputVersion: parent.InputVersion,
		BaselineRecords:  cloneRecords(parent.Records),
		Reports:          []CompareReport{},
	}
	// 延续父分支派生点之后已经模拟出的候选输入段，来源标注为父分支模拟。
	for _, record := range parent.Records {
		if record.Scan <= forkScan {
			continue
		}
		child.Segment = append(child.Segment, cloneBools(record.Inputs))
		child.Origins = append(child.Origins, RowOrigin{
			Scan:             record.Scan,
			Source:           "parent",
			SourceBranchID:   parent.ID,
			SourceBranchName: parent.Name,
			Inputs:           cloneBools(record.Inputs),
			Note:             fmt.Sprintf("继承自父分支 %s 的模拟输入（非真实扫描）", parent.Name),
		})
	}
	m.branches = append(m.branches, child)
	m.controller.addLog(fmt.Sprintf("已从父分支 %s 在扫描 #%d 派生子分支 %s", parent.ID, forkScan, child.ID))
	if len(child.Segment) > 0 {
		if err := m.simulate(child); err != nil {
			m.branches = m.branches[:len(m.branches)-1]
			return nil, err
		}
		child.Status = branchStatusApplied
	}
	return child, nil
}

// AbandonBranch 放弃一个中间分支：记录保留、只读，不再接受替换或派生，
// 但不会删除任何扫描记录，也不影响其他分支与原执行。有子分支时同样
// 可以放弃：子分支的共同前缀与对照依据在派生时已固定为快照，父分支
// 转为只读保留记录后，已有后代仍可继续替换、派生与对照。
func (m *BranchManager) AbandonBranch(branchID string) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if branch.Status == branchStatusAbandoned {
		return branch, nil
	}
	descendants := 0
	for _, other := range m.branches {
		if other.ParentID == branch.ID {
			descendants++
		}
	}
	branch.Status = branchStatusAbandoned
	note := "该中间分支已被用户放弃（记录只读保留）"
	if descendants > 0 {
		note += fmt.Sprintf("；其 %d 个已派生后代的对照依据已固定为快照，可继续工作", descendants)
	}
	branch.OverlapNotes = append(branch.OverlapNotes, note)
	m.controller.addLog(fmt.Sprintf("分支 %s 已放弃，历史记录保留", branch.ID))
	return branch, nil
}

// ReplaceSegment 替换分支中从 fromScan 起的一段虚拟输入并重新模拟。
// fromScan 之前的分支记录属于共同前缀，只读，不允许被替换覆盖。
func (m *BranchManager) ReplaceSegment(branchID string, fromScan int, inputs []map[string]bool) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if branch.Status == branchStatusAbandoned {
		return nil, fmt.Errorf("分支 %s 已放弃，替换输入为只读历史，不能再操作", branchID)
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
	oldOrigins := append([]RowOrigin{}, branch.Origins...)
	oldSegment := append([]map[string]bool{}, branch.Segment...)
	notes := []string{}
	for i := range inputs {
		scan := fromScan + i
		if prefix+i < len(oldOrigins) {
			notes = append(notes, fmt.Sprintf("扫描 #%d 与已模拟范围重叠：原候选（%s）作废，本次实际采用新提交的输入；未执行的候选不算真实扫描",
				scan, originLabel(oldOrigins[prefix+i])))
		}
	}
	segment := append(append([]map[string]bool{}, branch.Segment[:prefix]...), inputs...)
	origins := append([]RowOrigin{}, oldOrigins[:min(prefix, len(oldOrigins))]...)
	for i, row := range inputs {
		origins = append(origins, RowOrigin{
			Scan:   fromScan + i,
			Source: "self",
			Inputs: cloneBools(row),
			Note:   fmt.Sprintf("输入版本 #%d：本次替换实际采用", branch.InputVersion+1),
		})
	}
	branch.Segment = segment
	branch.Origins = origins
	branch.InputVersion++
	branch.OverlapNotes = notes
	if err := m.simulate(branch); err != nil {
		// 模拟失败：回滚本次替换，保留此前已生效的输入版本与模拟结果。
		branch.Segment = append([]map[string]bool{}, oldSegment...)
		branch.Origins = oldOrigins
		branch.InputVersion--
		branch.OverlapNotes = []string{}
		_ = m.simulate(branch)
		return nil, err
	}
	branch.Status = branchStatusApplied
	// 父分支替换输入后，已派生子分支的共同前缀与对照依据仍固定在
	// 各自派生时的快照上，不随本次替换悄悄变化；明确标注这一点。
	for _, other := range m.branches {
		if other.ParentID == branch.ID && other.Status != branchStatusAbandoned {
			branch.OverlapNotes = append(branch.OverlapNotes, fmt.Sprintf(
				"子分支 %s 的对照依据仍固定在其派生时的输入版本 #%d 快照（当前父分支已为输入版本 #%d）；如需按新结果对照，请在子分支上显式建立新的对照版本",
				other.Name, other.BaseInputVersion, branch.InputVersion))
		}
	}
	m.controller.addLog(fmt.Sprintf("分支 %s 已替换自扫描 #%d 起的 %d 个扫描输入（输入版本 #%d）", branch.ID, fromScan, len(inputs), branch.InputVersion))
	return branch, nil
}

func originLabel(origin RowOrigin) string {
	switch origin.Source {
	case "original":
		return "原执行真实扫描"
	case "parent":
		if origin.SourceBranchName != "" {
			return "父分支 " + origin.SourceBranchName + " 模拟（非真实扫描）"
		}
		return "父分支模拟（非真实扫描）"
	case "self":
		return "本分支之前的替换候选"
	default:
		return "未知来源"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RestartElement 在分支起点将某个元件的状态明确清零（定时器累计或
// 保持值），并留下记录。跨分支起点的定时器默认保留累计依据，只有
// 用户显式重新开始才清零。
func (m *BranchManager) RestartElement(branchID, nodeID string) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if branch.Status == branchStatusAbandoned {
		return nil, fmt.Errorf("分支 %s 已放弃，不能再重新开始元件", branchID)
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
	m.applyReport(branch)
	return nil
}

// Rebaseline 在父分支后续替换输入后，由用户显式为子分支建立新的
// 对照版本：重新快照父分支当前记录并追加一份对照报告，旧报告保留。
func (m *BranchManager) Rebaseline(branchID string) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if branch.Status == branchStatusAbandoned {
		return nil, fmt.Errorf("分支 %s 已放弃，对照依据为只读历史，不能重建", branchID)
	}
	if branch.ParentID == "" {
		return nil, errors.New("根分支对照原执行实时历史（只增不改），无需重建对照版本")
	}
	parent := m.findBranch(branch.ParentID)
	if parent == nil {
		return nil, fmt.Errorf("父分支 %s 已不存在，无法重建对照", branch.ParentID)
	}
	branch.BaselineRecords = cloneRecords(parent.Records)
	branch.BaseInputVersion = parent.InputVersion
	m.applyReport(branch)
	m.controller.addLog(fmt.Sprintf("分支 %s 已显式建立新的对照版本（父分支输入版本 #%d），旧报告保留",
		branch.ID, parent.InputVersion))
	return branch, nil
}

// applyReport 按当前固定的对照依据生成一份对照报告并追加到历史，
// 同时更新分支的最新差异字段。旧报告全部保留。
func (m *BranchManager) applyReport(branch *Branch) {
	report := m.compare(branch)
	branch.Reports = append(branch.Reports, report)
	branch.Divergence = report.Divergence
}

// baselineFor 返回分支固定的对照依据：子分支为派生/重建时父分支
// 记录的快照（父分支之后替换输入不影响）；根分支为原执行实时历史
// （只增不改，缺失记录不当作已执行扫描）。
func (m *BranchManager) baselineFor(branch *Branch) ([]ScanRecord, string) {
	if branch.ParentID != "" {
		name := fmt.Sprintf("父分支 %s（输入版本 #%d 快照）", branch.ParentName, branch.BaseInputVersion)
		if branch.BaselineRecords != nil {
			return branch.BaselineRecords, name
		}
		if parent := m.findBranch(branch.ParentID); parent != nil {
			return parent.Records, fmt.Sprintf("父分支 %s", parent.Name)
		}
		return nil, name
	}
	return m.controller.history, "原执行"
}

// compare 把分支记录与固定的对照依据（同程序修订、同扫描序号）逐扫描
// 比较，标出第一处可比较的真实输出差异及当时内部状态与定时累计。
// 对照依据缺失的扫描列为 missing：既不是已执行扫描，也不能据此判定一致。
func (m *BranchManager) compare(branch *Branch) CompareReport {
	baseline, baselineName := m.baselineFor(branch)
	report := CompareReport{
		ProgramVersion:     branch.BaseVersion,
		BaseInputVersion:   branch.BaseInputVersion,
		BranchInputVersion: branch.InputVersion,
		Missing:            []int{},
	}
	if len(branch.Records) > 0 {
		report.ScanFrom = branch.Records[0].Scan
		report.ScanTo = branch.Records[len(branch.Records)-1].Scan
	}
	scope := fmt.Sprintf("程序版本 %d，扫描范围 #%d–#%d", report.ProgramVersion, report.ScanFrom, report.ScanTo)
	for _, record := range branch.Records {
		base := findRecord(baseline, record.Scan, branch.BaseVersion)
		if base == nil {
			report.Missing = append(report.Missing, record.Scan)
			continue
		}
		report.Compared++
		outputs := diffKeys(record.Outputs, base.Outputs)
		internals := diffKeys(record.Internals, base.Internals)
		if len(outputs) == 0 {
			continue
		}
		divergence := &Divergence{
			Scan:          record.Scan,
			CompareTarget: baselineName,
			Outputs:       outputs,
			Internals:     internals,
			InternalState: cloneBools(record.Internals),
			TimerState:    cloneTimers(record.Timers),
			Message: fmt.Sprintf("与%s比较，扫描 #%d 输出首次出现差异：%s；当时内部状态：%s；定时累计：%s",
				baselineName, record.Scan, joinOrNone(outputs), formatState(record.Internals), formatTimers(record.Timers)),
		}
		report.Result = CompareResultDiverged
		report.Divergence = divergence
		report.Message = fmt.Sprintf("%s（对照固定：%s）%s", divergence.Message, scope, missingNote(report.Missing))
		return report
	}
	if report.Compared == 0 {
		report.Result = CompareResultNoBaseline
		report.Message = fmt.Sprintf("没有比较对象：%s 尚未覆盖扫描范围 #%d–#%d（%s）；缺失记录不视为已执行扫描，不能判定一致",
			baselineName, report.ScanFrom, report.ScanTo, scope)
		return report
	}
	report.Result = CompareResultIdentical
	report.Message = fmt.Sprintf("与%s已比较且输出一致：%d 个扫描（%s）%s",
		baselineName, report.Compared, scope, missingNote(report.Missing))
	return report
}

func missingNote(missing []int) string {
	if len(missing) == 0 {
		return ""
	}
	parts := make([]string, 0, len(missing))
	for _, scan := range missing {
		parts = append(parts, fmt.Sprintf("#%d", scan))
	}
	return fmt.Sprintf("；扫描 %s 没有比较对象（未执行的候选或缺失记录不当作已执行扫描）", strings.Join(parts, "、"))
}

// compareStale 判断父分支在子分支派生/重建对照后又替换了输入，
// 此时子分支的对照依据（快照）已过期，需显式建立新的对照版本。
func (m *BranchManager) compareStale(branch *Branch) bool {
	if branch.ParentID == "" {
		return false
	}
	parent := m.findBranch(branch.ParentID)
	return parent != nil && parent.InputVersion != branch.BaseInputVersion
}

func cloneRecords(records []ScanRecord) []ScanRecord {
	result := make([]ScanRecord, 0, len(records))
	for _, record := range records {
		result = append(result, ScanRecord{
			Scan:      record.Scan,
			Version:   record.Version,
			Inputs:    cloneBools(record.Inputs),
			Outputs:   cloneBools(record.Outputs),
			Internals: cloneBools(record.Internals),
			Timers:    cloneTimers(record.Timers),
		})
	}
	return result
}

func formatState(state map[string]bool) string {
	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s=%t", id, state[id]))
	}
	return strings.Join(parts, ", ")
}

func formatTimers(timers map[string]TimerState) string {
	ids := make([]string, 0, len(timers))
	for id := range timers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		timer := timers[id]
		parts = append(parts, fmt.Sprintf("%s=%dms/Q=%t", id, timer.ElapsedMs, timer.Q))
	}
	return strings.Join(parts, ", ")
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
		copy := *branch
		copy.CompareStale = m.compareStale(branch)
		result = append(result, copy)
	}
	return result
}

// FindTrials 按程序修订版本、输入版本与扫描序号找回各次试验。
// 任一条件为零值表示不限制；只返回匹配的边界与分支（含已放弃分支）。
func (m *BranchManager) FindTrials(programVersion, inputVersion, scan int) ([]Boundary, []Branch) {
	boundarySet := map[string]bool{}
	var boundaries []Boundary
	var branches []Branch
	for _, branch := range m.branches {
		if programVersion != 0 && branch.BaseVersion != programVersion {
			continue
		}
		if inputVersion != 0 && branch.InputVersion != inputVersion {
			continue
		}
		if scan != 0 && !(branch.BaseScan == scan || branch.ForkScan == scan) {
			matched := false
			for _, record := range branch.Records {
				if record.Scan == scan {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		copy := *branch
		copy.CompareStale = m.compareStale(branch)
		branches = append(branches, copy)
		if !boundarySet[branch.BoundaryID] {
			boundarySet[branch.BoundaryID] = true
			if boundary := m.findBoundary(branch.BoundaryID); boundary != nil {
				boundaries = append(boundaries, *boundary)
			}
		}
	}
	if boundaries == nil {
		boundaries = []Boundary{}
	}
	if branches == nil {
		branches = []Branch{}
	}
	return boundaries, branches
}

// MarshalJSON 保证所有切片字段序列化为 [] 而不是 null，避免前端
// 刚创建、尚无结果的分支读取 null.length 时白屏。
func (b Branch) MarshalJSON() ([]byte, error) {
	alias := struct {
		ID               string                `json:"id"`
		Name             string                `json:"name"`
		BoundaryID       string                `json:"boundaryId"`
		ParentID         string                `json:"parentId,omitempty"`
		ParentName       string                `json:"parentName,omitempty"`
		ForkScan         int                   `json:"forkScan,omitempty"`
		BaseScan         int                   `json:"baseScan"`
		BaseVersion      int                   `json:"baseVersion"`
		InputVersion     int                   `json:"inputVersion"`
		Status           string                `json:"status"`
		ProgramName      string                `json:"programName"`
		Program          Program               `json:"program"`
		BaseInputs       map[string]bool       `json:"baseInputs"`
		BaseValues       map[string]bool       `json:"baseValues"`
		BaseTimers       map[string]TimerState `json:"baseTimers"`
		Segment          []map[string]bool     `json:"segment"`
		Origins          []RowOrigin           `json:"origins"`
		Records          []ScanRecord          `json:"records"`
		Restarts         []ScanEvent           `json:"restarts"`
		OverlapNotes     []string              `json:"overlapNotes"`
		Divergence       *Divergence           `json:"divergence,omitempty"`
		BaseInputVersion int                   `json:"baseInputVersion"`
		BaselineRecords  []ScanRecord          `json:"baselineRecords"`
		Reports          []CompareReport       `json:"reports"`
		CompareStale     bool                  `json:"compareStale"`
	}{
		ID:               b.ID,
		Name:             b.Name,
		BoundaryID:       b.BoundaryID,
		ParentID:         b.ParentID,
		ParentName:       b.ParentName,
		ForkScan:         b.ForkScan,
		BaseScan:         b.BaseScan,
		BaseVersion:      b.BaseVersion,
		InputVersion:     b.InputVersion,
		Status:           b.Status,
		ProgramName:      b.ProgramName,
		Program:          b.Program,
		BaseInputs:       b.BaseInputs,
		BaseValues:       b.BaseValues,
		BaseTimers:       b.BaseTimers,
		Segment:          b.Segment,
		Origins:          b.Origins,
		Records:          b.Records,
		Restarts:         b.Restarts,
		OverlapNotes:     b.OverlapNotes,
		Divergence:       b.Divergence,
		BaseInputVersion: b.BaseInputVersion,
		BaselineRecords:  b.BaselineRecords,
		Reports:          b.Reports,
		CompareStale:     b.CompareStale,
	}
	if alias.Segment == nil {
		alias.Segment = []map[string]bool{}
	}
	if alias.Origins == nil {
		alias.Origins = []RowOrigin{}
	}
	if alias.Records == nil {
		alias.Records = []ScanRecord{}
	}
	if alias.Restarts == nil {
		alias.Restarts = []ScanEvent{}
	}
	if alias.OverlapNotes == nil {
		alias.OverlapNotes = []string{}
	}
	if alias.BaselineRecords == nil {
		alias.BaselineRecords = []ScanRecord{}
	}
	if alias.Reports == nil {
		alias.Reports = []CompareReport{}
	}
	for i := range alias.Reports {
		if alias.Reports[i].Missing == nil {
			alias.Reports[i].Missing = []int{}
		}
	}
	return json.Marshal(alias)
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
	for _, branch := range m.branches {
		if branch.Segment == nil {
			branch.Segment = []map[string]bool{}
		}
		if branch.Origins == nil {
			branch.Origins = []RowOrigin{}
		}
		if branch.Records == nil {
			branch.Records = []ScanRecord{}
		}
		if branch.Restarts == nil {
			branch.Restarts = []ScanEvent{}
		}
		if branch.OverlapNotes == nil {
			branch.OverlapNotes = []string{}
		}
		if branch.Reports == nil {
			branch.Reports = []CompareReport{}
		}
		for i := range branch.Reports {
			if branch.Reports[i].Missing == nil {
				branch.Reports[i].Missing = []int{}
			}
		}
		if branch.Status == "" {
			if len(branch.Records) > 0 {
				branch.Status = branchStatusApplied
			} else {
				branch.Status = branchStatusCreated
			}
		}
		// 旧存储没有对照报告：按当前固定的对照依据补一份，
		// 缺失的对照区间会明确标为没有比较对象，而不是视为一致。
		if len(branch.Records) > 0 && len(branch.Reports) == 0 {
			m.applyReport(branch)
		}
	}
	return nil
}
