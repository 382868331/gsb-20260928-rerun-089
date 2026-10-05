package main

import (
	"fmt"
	"sort"
	"time"
)

const (
	reviewPackageKind    = "branch-review-package"
	reviewPackageVersion = 2
	reviewScanCompared   = "compared"
	reviewScanMissing    = "missing"
)

// ReviewPackage 是分支对照的自包含审阅快照。它冻结当前报告、历史报告、
// 实际参与比较的扫描、缺失区间和程序信息；父分支继续更新或中间层被放弃后，
// 已导出的包仍可独立导入查看。
type ReviewPackage struct {
	Kind               string         `json:"kind"`
	FormatVersion      int            `json:"formatVersion"`
	ID                 string         `json:"id"`
	OriginalID         string         `json:"originalId,omitempty"`
	BranchID           string         `json:"branchId"`
	BranchName         string         `json:"branchName"`
	BranchStatus       string         `json:"branchStatus"`
	ParentID           string         `json:"parentId,omitempty"`
	ParentName         string         `json:"parentName,omitempty"`
	BaseScan           int            `json:"baseScan"`
	ForkScan           int            `json:"forkScan,omitempty"`
	ProgramVersion     int            `json:"programVersion"`
	InputVersion       int            `json:"inputVersion"`
	Program            Program        `json:"program"`
	ExportedAt         string         `json:"exportedAt"`
	CurrentReport      Comparison     `json:"currentReport"`
	HistoricalReports  []Comparison   `json:"historicalReports"`
	ComparedScans      []ReviewScan   `json:"comparedScans"`
	MissingScans       []ReviewScan   `json:"missingScans"`
	MissingIntervals   []ScanInterval `json:"missingIntervals"`
	BindingDescription string         `json:"bindingDescription"`
	Notes              []ReviewNote   `json:"notes"`
}

type ReviewScan struct {
	Scan                  int                   `json:"scan"`
	Status                string                `json:"status"`
	Candidate             ScanRecord            `json:"candidate"`
	Baseline              *ScanRecord           `json:"baseline,omitempty"`
	OutputDifferences     []string              `json:"outputDifferences"`
	InternalDifferences   []string              `json:"internalDifferences"`
	FirstOutputDifference bool                  `json:"firstOutputDifference"`
	TimerState            map[string]TimerState `json:"timerState"`
}

type ScanInterval struct {
	FromScan int `json:"fromScan"`
	ToScan   int `json:"toScan"`
}

type ReviewNote struct {
	ID             string `json:"id"`
	CompareVersion int    `json:"compareVersion"`
	Scan           int    `json:"scan,omitempty"`
	Text           string `json:"text"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

func (m *BranchManager) ExportReviewPackage(branchID string) (*ReviewPackage, error) {
	branch := m.findBranch(branchID)
	if branch == nil {
		return nil, fmt.Errorf("试验分支 %s 不存在", branchID)
	}
	if branch.Comparison == nil {
		return nil, fmt.Errorf("分支 %s 尚无对照报告，不能导出审阅包", branchID)
	}
	baseline := m.reviewBaseline(branch)
	pkg := &ReviewPackage{
		Kind:               reviewPackageKind,
		FormatVersion:      reviewPackageVersion,
		ID:                 m.allocID("review"),
		BranchID:           branch.ID,
		BranchName:         branch.Name,
		BranchStatus:       branch.Status,
		ParentID:           branch.ParentID,
		ParentName:         branch.ParentName,
		BaseScan:           branch.BaseScan,
		ForkScan:           branch.ForkScan,
		ProgramVersion:     branch.BaseVersion,
		InputVersion:       branch.InputVersion,
		Program:            branch.Program,
		ExportedAt:         nowRFC3339(),
		CurrentReport:      *branch.Comparison,
		HistoricalReports:  append([]Comparison{}, branch.Reports...),
		ComparedScans:      buildComparedReviewScans(branch, baseline),
		MissingScans:       buildMissingReviewScans(branch),
		MissingIntervals:   compressIntervals(branch.Comparison.MissingScans),
		BindingDescription: "审阅说明绑定到本包内的对照报告版本；scan 为 0 时说明适用于整份报告，否则只绑定该报告下的扫描序号。实际比较与缺失对照分开保存，缺失扫描不补写基线。",
		Notes:              []ReviewNote{},
	}
	normalizeReviewPackage(pkg)
	sort.SliceStable(pkg.HistoricalReports, func(i, j int) bool {
		return pkg.HistoricalReports[i].CompareVersion > pkg.HistoricalReports[j].CompareVersion
	})
	m.reviews = append(m.reviews, pkg)
	return pkg, nil
}

func (m *BranchManager) ImportReviewPackage(pkg ReviewPackage) (*ReviewPackage, error) {
	if pkg.Kind != reviewPackageKind {
		return nil, fmt.Errorf("不是支持的分支审阅包类型：%s", pkg.Kind)
	}
	if pkg.FormatVersion < 1 || pkg.FormatVersion > reviewPackageVersion {
		return nil, fmt.Errorf("不支持的审阅包格式版本：%d", pkg.FormatVersion)
	}
	if pkg.BranchID == "" || pkg.BranchName == "" {
		return nil, fmt.Errorf("审阅包缺少分支标识")
	}
	if _, err := compileProgram(pkg.Program); err != nil {
		return nil, fmt.Errorf("审阅包程序无法编译：%w", err)
	}
	if pkg.ExportedAt == "" {
		pkg.ExportedAt = nowRFC3339()
	}
	pkg.OriginalID = pkg.ID
	pkg.ID = m.allocID("review")
	if pkg.FormatVersion == 1 {
		migrateReviewPackageV1(&pkg)
	}
	pkg.FormatVersion = reviewPackageVersion
	if pkg.HistoricalReports == nil {
		pkg.HistoricalReports = []Comparison{}
	}
	if pkg.ComparedScans == nil {
		pkg.ComparedScans = []ReviewScan{}
	}
	if pkg.MissingScans == nil {
		pkg.MissingScans = []ReviewScan{}
	}
	if pkg.MissingIntervals == nil {
		pkg.MissingIntervals = []ScanInterval{}
	}
	if pkg.BindingDescription == "" {
		pkg.BindingDescription = "审阅说明绑定到本包内的对照报告版本；scan 为 0 时说明适用于整份报告，否则只绑定该报告下的扫描序号。实际比较与缺失对照分开保存，缺失扫描不补写基线。"
	}
	if pkg.Notes == nil {
		pkg.Notes = []ReviewNote{}
	}
	normalizeReviewPackage(&pkg)
	for i := range pkg.Notes {
		if pkg.Notes[i].ID == "" {
			pkg.Notes[i].ID = m.allocID("note")
		}
	}
	m.reviews = append(m.reviews, &pkg)
	return &pkg, nil
}

func (m *BranchManager) AddReviewNote(reviewID string, compareVersion, scan int, text string) (*ReviewPackage, error) {
	if text == "" {
		return nil, fmt.Errorf("审阅说明不能为空")
	}
	pkg := m.findReview(reviewID)
	if pkg == nil {
		return nil, fmt.Errorf("审阅包 %s 不存在", reviewID)
	}
	versions := map[int]bool{pkg.CurrentReport.CompareVersion: true}
	for _, report := range pkg.HistoricalReports {
		versions[report.CompareVersion] = true
	}
	if !versions[compareVersion] {
		return nil, fmt.Errorf("审阅包中没有对照版本 #%d", compareVersion)
	}
	if scan != 0 {
		found := false
		for _, row := range append(append([]ReviewScan{}, pkg.ComparedScans...), pkg.MissingScans...) {
			if row.Scan == scan {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("扫描 #%d 不在审阅包范围内", scan)
		}
	}
	now := nowRFC3339()
	pkg.Notes = append(pkg.Notes, ReviewNote{
		ID:             m.allocID("note"),
		CompareVersion: compareVersion,
		Scan:           scan,
		Text:           text,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	return pkg, nil
}

func (m *BranchManager) ReviewPackages() []ReviewPackage {
	result := make([]ReviewPackage, 0, len(m.reviews))
	for _, pkg := range m.reviews {
		result = append(result, *pkg)
	}
	return result
}

func (m *BranchManager) findReview(id string) *ReviewPackage {
	for _, pkg := range m.reviews {
		if pkg.ID == id || pkg.OriginalID == id {
			return pkg
		}
	}
	return nil
}

func (m *BranchManager) reviewBaseline(branch *Branch) []ScanRecord {
	if branch.ParentID != "" {
		return branch.Baseline
	}
	return m.controller.history
}

func buildComparedReviewScans(branch *Branch, baseline []ScanRecord) []ReviewScan {
	result := make([]ReviewScan, 0, len(branch.Records))
	firstDifference := 0
	if branch.Comparison != nil && branch.Comparison.Divergence != nil {
		firstDifference = branch.Comparison.Divergence.Scan
	}
	for _, candidate := range branch.Records {
		base := findRecord(baseline, candidate.Scan, branch.BaseVersion)
		if base == nil {
			continue
		}
		baseCopy := cloneScanRecord(*base)
		candidateCopy := cloneScanRecord(candidate)
		row := ReviewScan{
			Scan:                  candidateCopy.Scan,
			Status:                reviewScanCompared,
			Candidate:             candidateCopy,
			Baseline:              &baseCopy,
			OutputDifferences:     diffKeys(candidateCopy.Outputs, baseCopy.Outputs),
			InternalDifferences:   diffKeys(candidateCopy.Internals, baseCopy.Internals),
			FirstOutputDifference: candidateCopy.Scan == firstDifference,
			TimerState:            cloneTimers(candidateCopy.Timers),
		}
		result = append(result, row)
	}
	return result
}

func buildMissingReviewScans(branch *Branch) []ReviewScan {
	missing := map[int]bool{}
	for _, scan := range branch.Comparison.MissingScans {
		missing[scan] = true
	}
	result := make([]ReviewScan, 0, len(missing))
	for _, candidate := range branch.Records {
		if !missing[candidate.Scan] {
			continue
		}
		candidateCopy := cloneScanRecord(candidate)
		result = append(result, ReviewScan{
			Scan:                candidateCopy.Scan,
			Status:              reviewScanMissing,
			Candidate:           candidateCopy,
			OutputDifferences:   []string{},
			InternalDifferences: []string{},
			TimerState:          cloneTimers(candidateCopy.Timers),
		})
	}
	return result
}

func cloneScanRecord(record ScanRecord) ScanRecord {
	return ScanRecord{
		Scan:      record.Scan,
		Version:   record.Version,
		Inputs:    cloneBools(record.Inputs),
		Outputs:   cloneBools(record.Outputs),
		Internals: cloneBools(record.Internals),
		Timers:    cloneTimers(record.Timers),
	}
}

func migrateReviewPackageV1(pkg *ReviewPackage) {
	missing := pkg.MissingScans
	compared := make([]ReviewScan, 0, len(pkg.ComparedScans))
	for _, row := range pkg.ComparedScans {
		if row.Status == reviewScanMissing {
			row.Baseline = nil
			row.OutputDifferences = []string{}
			row.InternalDifferences = []string{}
			row.FirstOutputDifference = false
			missing = append(missing, row)
			continue
		}
		row.Status = reviewScanCompared
		compared = append(compared, row)
	}
	pkg.ComparedScans = compared
	pkg.MissingScans = missing
	if len(pkg.MissingIntervals) == 0 {
		scans := make([]int, 0, len(pkg.MissingScans))
		for _, row := range pkg.MissingScans {
			scans = append(scans, row.Scan)
		}
		pkg.MissingIntervals = compressIntervals(scans)
	}
}

func compressIntervals(scans []int) []ScanInterval {
	if len(scans) == 0 {
		return []ScanInterval{}
	}
	sorted := append([]int{}, scans...)
	sort.Ints(sorted)
	var intervals []ScanInterval
	start, end := sorted[0], sorted[0]
	for _, scan := range sorted[1:] {
		if scan == end+1 {
			end = scan
			continue
		}
		intervals = append(intervals, ScanInterval{FromScan: start, ToScan: end})
		start, end = scan, scan
	}
	intervals = append(intervals, ScanInterval{FromScan: start, ToScan: end})
	return intervals
}

func nowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}

func normalizeReviewPackage(pkg *ReviewPackage) {
	if pkg.Program.Nodes == nil {
		pkg.Program.Nodes = []Node{}
	}
	if pkg.Program.Edges == nil {
		pkg.Program.Edges = []Edge{}
	}
	normalizeComparison(&pkg.CurrentReport)
	for i := range pkg.HistoricalReports {
		normalizeComparison(&pkg.HistoricalReports[i])
	}
	if pkg.HistoricalReports == nil {
		pkg.HistoricalReports = []Comparison{}
	}
	if pkg.ComparedScans == nil {
		pkg.ComparedScans = []ReviewScan{}
	}
	if pkg.MissingScans == nil {
		pkg.MissingScans = []ReviewScan{}
	}
	if pkg.MissingIntervals == nil {
		pkg.MissingIntervals = []ScanInterval{}
	}
	if pkg.BindingDescription == "" {
		pkg.BindingDescription = "审阅说明绑定到本包内的对照报告版本；scan 为 0 时说明适用于整份报告，否则只绑定该报告下的扫描序号。实际比较与缺失对照分开保存，缺失扫描不补写基线。"
	}
	if pkg.Notes == nil {
		pkg.Notes = []ReviewNote{}
	}
	for i := range pkg.ComparedScans {
		normalizeReviewScan(&pkg.ComparedScans[i], reviewScanCompared)
	}
	for i := range pkg.MissingScans {
		normalizeReviewScan(&pkg.MissingScans[i], reviewScanMissing)
	}
}

func normalizeComparison(report *Comparison) {
	if report.MissingScans == nil {
		report.MissingScans = []int{}
	}
	if report.Divergence != nil {
		if report.Divergence.Outputs == nil {
			report.Divergence.Outputs = []string{}
		}
		if report.Divergence.Internals == nil {
			report.Divergence.Internals = []string{}
		}
		if report.Divergence.InternalState == nil {
			report.Divergence.InternalState = map[string]bool{}
		}
		if report.Divergence.TimerState == nil {
			report.Divergence.TimerState = map[string]TimerState{}
		}
	}
}

func normalizeReviewScan(row *ReviewScan, status string) {
	if row.Status != reviewScanCompared && row.Status != reviewScanMissing {
		row.Status = status
	}
	if row.Candidate.Inputs == nil {
		row.Candidate.Inputs = map[string]bool{}
	}
	if row.Candidate.Outputs == nil {
		row.Candidate.Outputs = map[string]bool{}
	}
	if row.Candidate.Internals == nil {
		row.Candidate.Internals = map[string]bool{}
	}
	if row.Candidate.Timers == nil {
		row.Candidate.Timers = map[string]TimerState{}
	}
	if row.TimerState == nil {
		row.TimerState = map[string]TimerState{}
	}
	if row.OutputDifferences == nil {
		row.OutputDifferences = []string{}
	}
	if row.InternalDifferences == nil {
		row.InternalDifferences = []string{}
	}
	if row.Status == reviewScanMissing {
		row.Baseline = nil
		row.OutputDifferences = []string{}
		row.InternalDifferences = []string{}
		row.FirstOutputDifference = false
	}
}
