package main

import (
	"fmt"
	"sort"
	"time"
)

const (
	reviewPackageKind    = "branch-review-package"
	reviewPackageVersion = 1
	reviewScanCompared   = "compared"
	reviewScanMissing    = "missing"
)

// ReviewPackage 是分支对照的自包含审阅快照。它冻结当前报告、历史报告、
// 实际参与比较的扫描、缺失区间和程序信息；父分支继续更新或中间层被放弃后，
// 已导出的包仍可独立导入查看。
type ReviewPackage struct {
	Kind              string         `json:"kind"`
	FormatVersion     int            `json:"formatVersion"`
	ID                string         `json:"id"`
	OriginalID        string         `json:"originalId,omitempty"`
	BranchID          string         `json:"branchId"`
	BranchName        string         `json:"branchName"`
	BranchStatus      string         `json:"branchStatus"`
	ParentID          string         `json:"parentId,omitempty"`
	ParentName        string         `json:"parentName,omitempty"`
	BaseScan          int            `json:"baseScan"`
	ForkScan          int            `json:"forkScan,omitempty"`
	ProgramVersion    int            `json:"programVersion"`
	InputVersion      int            `json:"inputVersion"`
	Program           Program        `json:"program"`
	ExportedAt        string         `json:"exportedAt"`
	CurrentReport     Comparison     `json:"currentReport"`
	HistoricalReports []Comparison   `json:"historicalReports"`
	ComparedScans     []ReviewScan   `json:"comparedScans"`
	MissingIntervals  []ScanInterval `json:"missingIntervals"`
	Notes             []ReviewNote   `json:"notes"`
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
		Kind:              reviewPackageKind,
		FormatVersion:     reviewPackageVersion,
		ID:                m.allocID("review"),
		BranchID:          branch.ID,
		BranchName:        branch.Name,
		BranchStatus:      branch.Status,
		ParentID:          branch.ParentID,
		ParentName:        branch.ParentName,
		BaseScan:          branch.BaseScan,
		ForkScan:          branch.ForkScan,
		ProgramVersion:    branch.BaseVersion,
		InputVersion:      branch.InputVersion,
		Program:           branch.Program,
		ExportedAt:        nowRFC3339(),
		CurrentReport:     *branch.Comparison,
		HistoricalReports: append([]Comparison{}, branch.Reports...),
		ComparedScans:     buildReviewScans(branch, baseline),
		MissingIntervals:  compressIntervals(branch.Comparison.MissingScans),
		Notes:             []ReviewNote{},
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
	if pkg.FormatVersion != reviewPackageVersion {
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
		for _, row := range pkg.ComparedScans {
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

func buildReviewScans(branch *Branch, baseline []ScanRecord) []ReviewScan {
	result := make([]ReviewScan, 0, len(branch.Records))
	firstDifference := 0
	if branch.Comparison != nil && branch.Comparison.Divergence != nil {
		firstDifference = branch.Comparison.Divergence.Scan
	}
	for _, candidate := range branch.Records {
		base := findRecord(baseline, candidate.Scan, branch.BaseVersion)
		row := ReviewScan{
			Scan:       candidate.Scan,
			Candidate:  candidate,
			TimerState: cloneTimers(candidate.Timers),
		}
		if base == nil {
			row.Status = reviewScanMissing
			row.OutputDifferences = []string{}
			row.InternalDifferences = []string{}
		} else {
			row.Status = reviewScanCompared
			baseCopy := *base
			row.Baseline = &baseCopy
			row.OutputDifferences = diffKeys(candidate.Outputs, base.Outputs)
			row.InternalDifferences = diffKeys(candidate.Internals, base.Internals)
			row.FirstOutputDifference = candidate.Scan == firstDifference
		}
		if row.OutputDifferences == nil {
			row.OutputDifferences = []string{}
		}
		if row.InternalDifferences == nil {
			row.InternalDifferences = []string{}
		}
		if row.TimerState == nil {
			row.TimerState = map[string]TimerState{}
		}
		result = append(result, row)
	}
	return result
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
	if pkg.HistoricalReports == nil {
		pkg.HistoricalReports = []Comparison{}
	}
	if pkg.ComparedScans == nil {
		pkg.ComparedScans = []ReviewScan{}
	}
	if pkg.MissingIntervals == nil {
		pkg.MissingIntervals = []ScanInterval{}
	}
	if pkg.Notes == nil {
		pkg.Notes = []ReviewNote{}
	}
	if pkg.CurrentReport.MissingScans == nil {
		pkg.CurrentReport.MissingScans = []int{}
	}
	normalizeComparison(&pkg.CurrentReport)
	for i := range pkg.HistoricalReports {
		normalizeComparison(&pkg.HistoricalReports[i])
	}
	for i := range pkg.ComparedScans {
		row := &pkg.ComparedScans[i]
		if row.OutputDifferences == nil {
			row.OutputDifferences = []string{}
		}
		if row.InternalDifferences == nil {
			row.InternalDifferences = []string{}
		}
		if row.TimerState == nil {
			row.TimerState = map[string]TimerState{}
		}
	}
}

func normalizeComparison(report *Comparison) {
	if report.MissingScans == nil {
		report.MissingScans = []int{}
	}
	if report.Divergence == nil {
		return
	}
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
