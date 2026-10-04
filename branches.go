package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
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
		ID:             m.allocID("branch"),
		Name:           name,
		BoundaryID:     boundary.ID,
		BaseScan:       boundary.Scan,
		BaseVersion:    boundary.Version,
		InputVersion:   0,
		Status:         branchStatusCreated,
		ProgramName:    boundary.Program.Name,
		Program:        boundary.Program,
		BaseInputs:     cloneBools(boundary.Inputs),
		BaseValues:     cloneBools(boundary.Values),
		BaseTimers:     cloneTimers(boundary.Timers),
		Segment:        []map[string]bool{},
		Origins:        []RowOrigin{},
		Records:        []ScanRecord{},
		Restarts:       []ScanEvent{},
		OverlapNotes:   []string{},
		Baseline:       []ScanRecord{},
		Reports:        []Comparison{},
		ReviewNotes:    []ReviewNote{},
		CompareVersion: 1,
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
		ID:             m.allocID("branch"),
		Name:           name,
		BoundaryID:     parent.BoundaryID,
		ParentID:       parent.ID,
		ParentName:     parent.Name,
		ForkScan:       forkScan,
		BaseScan:       forkScan,
		BaseVersion:    parent.BaseVersion,
		InputVersion:   0,
		Status:         branchStatusCreated,
		ProgramName:    parent.ProgramName,
		Program:        parent.Program,
		BaseInputs:     cloneBools(forkRecord.Inputs),
		BaseValues:     values,
		BaseTimers:     cloneTimers(forkRecord.Timers),
		Segment:        []map[string]bool{},
		Origins:        []RowOrigin{},
		Records:        []ScanRecord{},
		Restarts:       []ScanEvent{},
		OverlapNotes:   []string{},
		Baseline:       []ScanRecord{},
		Reports:        []Comparison{},
		ReviewNotes:    []ReviewNote{},
		CompareVersion: 1,
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
	// 冻结对照快照：父分支之后替换输入不会悄悄改变本子分支的比较依据；
	// 如需按父分支新输入对照，必须在子分支上显式建立新的对照版本。
	child.Baseline = cloneRecords(parent.Records)
	child.BaselineInputVersion = parent.InputVersion
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

// AbandonBranch 放弃一个中间分支：分支转为不可再编辑的只读保留记录，
// 不再接受替换、派生或重建对照，但不会删除任何扫描记录；已有后代分支
// 以其冻结的对照快照继续工作，不受放弃操作影响。
func (m *BranchManager) AbandonBranch(branchID string) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if branch.Status == branchStatusAbandoned {
		return branch, nil
	}
	branch.Status = branchStatusAbandoned
	note := "该中间分支已被用户放弃，转为只读保留记录，不能再编辑"
	children := 0
	for _, other := range m.branches {
		if other.ParentID == branch.ID && other.Status != branchStatusAbandoned {
			children++
		}
	}
	if children > 0 {
		note += fmt.Sprintf("；其 %d 个后代分支以冻结的对照快照继续有效", children)
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
	for _, other := range m.branches {
		if other.ParentID == branch.ID && other.Status != branchStatusAbandoned {
			notes = append(notes, fmt.Sprintf("子分支 %s 的对照仍冻结在本分支输入版本 #%d 的快照，本次替换不会悄悄改变其比较依据；如需按新输入对照，请在该子分支上显式建立新的对照版本（旧报告保留）",
				other.Name, other.BaselineInputVersion))
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
	branch.Comparison = m.compare(branch)
	branch.Divergence = branch.Comparison.Divergence
	return nil
}

// compare 把分支记录与固定的对照基线逐扫描比较，给出完整对照结论：
// 基线固定为程序修订版本 + 输入版本 + 扫描范围（子分支用派生或重建
// 对照时冻结的父分支快照，顶层分支用原执行只读历史）。明确区分三种
// 结果：确实一致、存在真实差异、没有比较对象——候选输入或缺失记录
// 不会被当作已执行扫描而误判为一致。
func (m *BranchManager) compare(branch *Branch) *Comparison {
	var baselineName string
	var baseline []ScanRecord
	parentInputVersion := 0
	if branch.ParentID != "" {
		baseline = branch.Baseline
		parentInputVersion = branch.BaselineInputVersion
		baselineName = fmt.Sprintf("父分支 %s（输入版本 #%d 的冻结快照）", branch.ParentName, branch.BaselineInputVersion)
	} else {
		baseline = m.controller.history
		baselineName = "原执行"
	}
	comparison := &Comparison{
		CompareVersion:      branch.CompareVersion,
		CompareTarget:       baselineName,
		ProgramVersion:      branch.BaseVersion,
		InputVersion:        branch.InputVersion,
		ParentInputVersion:  parentInputVersion,
		MissingScans:        []int{},
		MissingRanges:       []ScanRange{},
		ComparedScanNumbers: []int{},
		CandidateRecords:    cloneRecords(branch.Records),
		BaselineRecords:     cloneRecords(baseline),
	}
	if len(branch.Records) > 0 {
		comparison.FromScan = branch.Records[0].Scan
		comparison.ToScan = branch.Records[len(branch.Records)-1].Scan
	}
	for _, record := range branch.Records {
		base := findRecord(baseline, record.Scan, branch.BaseVersion)
		if base == nil {
			// 对照基线中没有该扫描的记录：这不是已执行扫描，不能算作一致。
			comparison.MissingScans = append(comparison.MissingScans, record.Scan)
			continue
		}
		comparison.ComparedScans++
		comparison.ComparedScanNumbers = append(comparison.ComparedScanNumbers, record.Scan)
		if comparison.Divergence != nil {
			continue
		}
		outputs := diffKeys(record.Outputs, base.Outputs)
		if len(outputs) == 0 {
			continue
		}
		internals := diffKeys(record.Internals, base.Internals)
		comparison.Divergence = &Divergence{
			Scan:          record.Scan,
			CompareTarget: baselineName,
			Outputs:       outputs,
			Internals:     internals,
			InternalState: cloneBools(record.Internals),
			TimerState:    cloneTimers(record.Timers),
			Message: fmt.Sprintf("与%s比较，扫描 #%d 输出首次出现真实差异：%s；当时内部状态：%s；定时累计：%s",
				baselineName, record.Scan, joinOrNone(outputs), formatState(record.Internals), formatTimers(record.Timers)),
		}
	}
	comparison.MissingRanges = rangesFromScans(comparison.MissingScans)
	switch {
	case comparison.Divergence != nil:
		comparison.Status = CompareStatusDiverged
		comparison.Message = comparison.Divergence.Message
	case comparison.ComparedScans == 0:
		comparison.Status = CompareStatusMissing
		comparison.Message = fmt.Sprintf("没有比较对象：%s在扫描范围 #%d-#%d 内没有任何可对照的已执行记录，不能视为一致",
			baselineName, comparison.FromScan, comparison.ToScan)
	default:
		comparison.Status = CompareStatusConsistent
		comparison.Message = fmt.Sprintf("确实一致：已与%s逐扫描比较 %d 个扫描（#%d-#%d），输出全部一致",
			baselineName, comparison.ComparedScans, comparison.FromScan, comparison.ToScan)
		if len(comparison.MissingScans) > 0 {
			comparison.Message += fmt.Sprintf("；注意扫描 %s 缺少对照记录，未计入一致结论",
				joinInts(comparison.MissingScans))
		}
	}
	return comparison
}

func rangesFromScans(scans []int) []ScanRange {
	if len(scans) == 0 {
		return []ScanRange{}
	}
	sortedScans := append([]int{}, scans...)
	sort.Ints(sortedScans)
	ranges := []ScanRange{}
	start := sortedScans[0]
	end := start
	for _, scan := range sortedScans[1:] {
		if scan == end+1 {
			end = scan
			continue
		}
		ranges = append(ranges, ScanRange{From: start, To: end})
		start, end = scan, scan
	}
	ranges = append(ranges, ScanRange{From: start, To: end})
	return ranges
}

func scansComparedWith(baseline, records []ScanRecord, version int) []int {
	result := []int{}
	for _, record := range records {
		if findRecord(baseline, record.Scan, version) != nil {
			result = append(result, record.Scan)
		}
	}
	return result
}

// AddReviewNote 为指定报告版本记录审阅说明。放弃分支仍允许补充说明，
// 但说明只追加到报告审阅历史，不改变任何模拟或比较结果。
func (m *BranchManager) AddReviewNote(branchID string, compareVersion int, text string) (*Branch, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("审阅说明不能为空")
	}
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if !m.branchHasReport(branch, compareVersion) {
		return nil, fmt.Errorf("分支 %s 没有对照报告版本 #%d", branchID, compareVersion)
	}
	branch.ReviewNotes = append(branch.ReviewNotes, ReviewNote{
		CompareVersion: compareVersion,
		CreatedAt:      time.Now().Format(time.RFC3339),
		Text:           text,
	})
	m.controller.addLog(fmt.Sprintf("分支 %s 的对照报告 #%d 已新增审阅说明", branch.ID, compareVersion))
	return branch, nil
}

func (m *BranchManager) branchHasReport(branch *Branch, compareVersion int) bool {
	if branch.Comparison != nil && branch.Comparison.CompareVersion == compareVersion {
		return true
	}
	for _, report := range branch.Reports {
		if report.CompareVersion == compareVersion {
			return true
		}
	}
	return false
}

// ExportReviewPackage 生成单个分支的自包含审阅包。包内同时放入当前报告、
// 历史报告、实际扫描列表、缺失区间和当时冻结的对照记录，不依赖父分支仍可打开。
func (m *BranchManager) ExportReviewPackage(branchID string) (*ReviewPackage, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if branch.Comparison == nil {
		return nil, fmt.Errorf("分支 %s 尚无对照报告，不能导出审阅包", branchID)
	}
	reports := make([]Comparison, 0, len(branch.Reports)+1)
	reports = append(reports, branch.Reports...)
	reports = append(reports, *branch.Comparison)
	sort.SliceStable(reports, func(i, j int) bool {
		return reports[i].CompareVersion < reports[j].CompareVersion
	})
	notes := append([]ReviewNote{}, branch.ReviewNotes...)
	pkg := &ReviewPackage{
		Kind:          "branch-review-package",
		FormatVersion: "1",
		ExportedAt:    time.Now().Format(time.RFC3339),
		Branch:        *branch,
		CurrentReport: *branch.Comparison,
		Reports:       reports,
		ReviewNotes:   notes,
	}
	if boundary := m.findBoundary(branch.BoundaryID); boundary != nil {
		copied := *boundary
		pkg.Boundary = &copied
	}
	return pkg, nil
}

// RebaseComparison 在子分支上显式建立新的对照版本：把对照基线刷新为
// 父分支当前的模拟记录与输入版本，旧对照报告移入只读历史保留。
// 父分支替换输入后，已有子分支的共同前缀与比较依据不会悄悄变化，
// 只有用户显式调用本操作才建立新的对照。
func (m *BranchManager) RebaseComparison(branchID string) (*Branch, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if branch.Status == branchStatusAbandoned {
		return nil, fmt.Errorf("分支 %s 已放弃，只读保留，不能再建立对照", branchID)
	}
	if branch.ParentID == "" {
		return nil, errors.New("顶层分支直接对照原执行只读历史，无需重建对照")
	}
	parent := m.findBranch(branch.ParentID)
	if parent == nil {
		return nil, fmt.Errorf("父分支 %s 已不存在，无法重建对照", branch.ParentID)
	}
	if len(parent.Records) == 0 {
		return nil, errors.New("父分支尚无模拟记录，不能作为对照基线")
	}
	if branch.Comparison != nil {
		archived := *branch.Comparison
		archived.CandidateRecords = cloneRecords(branch.Records)
		archived.BaselineRecords = cloneRecords(branch.Baseline)
		archived.MissingRanges = rangesFromScans(archived.MissingScans)
		if len(archived.ComparedScanNumbers) == 0 {
			archived.ComparedScanNumbers = scansComparedWith(branch.Baseline, branch.Records, branch.BaseVersion)
		}
		branch.Reports = append(branch.Reports, archived)
	}
	branch.Baseline = cloneRecords(parent.Records)
	branch.BaselineInputVersion = parent.InputVersion
	branch.CompareVersion++
	branch.Comparison = m.compare(branch)
	branch.Divergence = branch.Comparison.Divergence
	m.controller.addLog(fmt.Sprintf("分支 %s 已建立对照版本 #%d（对照父分支输入版本 #%d），旧报告保留",
		branch.ID, branch.CompareVersion, parent.InputVersion))
	return branch, nil
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

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("#%d", value))
	}
	return strings.Join(parts, ", ")
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
		result = append(result, *branch)
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
		branches = append(branches, *branch)
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
		ID                   string                `json:"id"`
		Name                 string                `json:"name"`
		BoundaryID           string                `json:"boundaryId"`
		ParentID             string                `json:"parentId,omitempty"`
		ParentName           string                `json:"parentName,omitempty"`
		ForkScan             int                   `json:"forkScan,omitempty"`
		BaseScan             int                   `json:"baseScan"`
		BaseVersion          int                   `json:"baseVersion"`
		InputVersion         int                   `json:"inputVersion"`
		Status               string                `json:"status"`
		ProgramName          string                `json:"programName"`
		Program              Program               `json:"program"`
		BaseInputs           map[string]bool       `json:"baseInputs"`
		BaseValues           map[string]bool       `json:"baseValues"`
		BaseTimers           map[string]TimerState `json:"baseTimers"`
		Segment              []map[string]bool     `json:"segment"`
		Origins              []RowOrigin           `json:"origins"`
		Records              []ScanRecord          `json:"records"`
		Restarts             []ScanEvent           `json:"restarts"`
		OverlapNotes         []string              `json:"overlapNotes"`
		Divergence           *Divergence           `json:"divergence,omitempty"`
		Baseline             []ScanRecord          `json:"baseline"`
		BaselineInputVersion int                   `json:"baselineInputVersion,omitempty"`
		CompareVersion       int                   `json:"compareVersion"`
		Comparison           *Comparison           `json:"comparison,omitempty"`
		Reports              []Comparison          `json:"reports"`
		ReviewNotes          []ReviewNote          `json:"reviewNotes"`
	}{
		ID:           b.ID,
		Name:         b.Name,
		BoundaryID:   b.BoundaryID,
		ParentID:     b.ParentID,
		ParentName:   b.ParentName,
		ForkScan:     b.ForkScan,
		BaseScan:     b.BaseScan,
		BaseVersion:  b.BaseVersion,
		InputVersion: b.InputVersion,
		Status:       b.Status,
		ProgramName:  b.ProgramName,
		Program:      b.Program,
		BaseInputs:   b.BaseInputs,
		BaseValues:   b.BaseValues,
		BaseTimers:   b.BaseTimers,
		Segment:      b.Segment,
		Origins:      b.Origins,
		Records:      b.Records,
		Restarts:     b.Restarts,
		OverlapNotes: b.OverlapNotes,
		Divergence:   b.Divergence,
	}
	alias.Baseline = b.Baseline
	alias.BaselineInputVersion = b.BaselineInputVersion
	alias.CompareVersion = b.CompareVersion
	alias.Comparison = b.Comparison
	alias.Reports = b.Reports
	alias.ReviewNotes = b.ReviewNotes
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
	if alias.Baseline == nil {
		alias.Baseline = []ScanRecord{}
	}
	if alias.Reports == nil {
		alias.Reports = []Comparison{}
	}
	if alias.ReviewNotes == nil {
		alias.ReviewNotes = []ReviewNote{}
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
		if branch.Baseline == nil {
			branch.Baseline = []ScanRecord{}
		}
		if branch.Reports == nil {
			branch.Reports = []Comparison{}
		}
		if branch.ReviewNotes == nil {
			branch.ReviewNotes = []ReviewNote{}
		}
		if branch.Status == "" {
			if len(branch.Records) > 0 {
				branch.Status = branchStatusApplied
			} else {
				branch.Status = branchStatusCreated
			}
		}
	}
	// 兼容旧存储：子分支缺少冻结对照快照时，从父分支当前记录重建，
	// 并补齐对照结论，保证重开后比较面板语义一致。
	for _, branch := range m.branches {
		if branch.ParentID != "" && len(branch.Baseline) == 0 {
			if parent := m.findBranch(branch.ParentID); parent != nil {
				branch.Baseline = cloneRecords(parent.Records)
				branch.BaselineInputVersion = parent.InputVersion
			}
		}
		if branch.CompareVersion < 1 {
			branch.CompareVersion = 1
		}
		if len(branch.Records) > 0 && branch.Comparison == nil {
			branch.Comparison = m.compare(branch)
			branch.Divergence = branch.Comparison.Divergence
		}
		normalizeComparison := func(report *Comparison) {
			if report.MissingScans == nil {
				report.MissingScans = []int{}
			}
			if report.ComparedScanNumbers == nil {
				report.ComparedScanNumbers = []int{}
			}
			if report.MissingRanges == nil {
				report.MissingRanges = rangesFromScans(report.MissingScans)
			}
			if len(report.BaselineRecords) == 0 {
				report.BaselineRecords = cloneRecords(branch.Baseline)
			}
		}
		if branch.Comparison != nil {
			normalizeComparison(branch.Comparison)
		}
		for i := range branch.Reports {
			normalizeComparison(&branch.Reports[i])
		}
	}
	return nil
}
