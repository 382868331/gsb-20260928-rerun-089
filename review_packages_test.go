package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestReviewPackageContainsPartialMissingScansAndReopensIndependently(t *testing.T) {
	controller, _ := NewController(demoProgram())
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	_ = controller.SetInput("start", false)
	controller.Scan()
	boundary := manager.SaveBoundary("")
	branch, _ := manager.CreateBranch(boundary.ID, "部分缺失对照")
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		controller.Scan()
	}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}, {}, {}}); err != nil {
		t.Fatal(err)
	}

	pkg, err := manager.ExportReviewPackage(branch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Kind != reviewPackageKind || len(pkg.ComparedScans) != 2 || len(pkg.MissingScans) != 1 {
		t.Fatalf("审阅包必须分开保存实际比较与缺失扫描: %+v", pkg)
	}
	for _, row := range pkg.ComparedScans {
		if row.Status != reviewScanCompared || row.Baseline == nil || row.TimerState == nil {
			t.Fatalf("实际比较扫描必须包含基线和定时器状态: %+v", row)
		}
	}
	for _, row := range pkg.MissingScans {
		if row.Status != reviewScanMissing || row.Baseline != nil || len(row.OutputDifferences) != 0 {
			t.Fatalf("缺失扫描不能补写未发生的对照: %+v", row)
		}
	}
	if len(pkg.MissingIntervals) != 1 || pkg.MissingIntervals[0].FromScan != branch.BaseScan+3 || pkg.MissingIntervals[0].ToScan != branch.BaseScan+3 {
		t.Fatalf("缺失区间必须压缩保留: %+v", pkg.MissingIntervals)
	}

	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	otherController, _ := NewController(demoProgram())
	other := NewBranchManager(otherController)
	var imported ReviewPackage
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatal(err)
	}
	reopened, err := other.ImportReviewPackage(imported)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ID == pkg.ID || reopened.OriginalID != pkg.ID {
		t.Fatal("重新打开应分配本地包 ID，同时保留原始导出 ID")
	}
	if pkg.BindingDescription == "" {
		t.Fatal("审阅包必须说明说明与报告/扫描的绑定关系")
	}
	if len(other.branches) != 0 || len(reopened.ComparedScans) != 2 || len(reopened.MissingScans) != 1 {
		t.Fatal("重新打开审阅包不能依赖当前分支，也不能丢失扫描明细")
	}
}

func TestReviewPackageMigratesV1MixedScansAndNullCollections(t *testing.T) {
	legacy := ReviewPackage{
		Kind:          reviewPackageKind,
		FormatVersion: 1,
		ID:            "review-old",
		BranchID:      "branch",
		BranchName:    "旧包",
		BranchStatus:  branchStatusApplied,
		BaseScan:      2,
		Program:       demoProgram(),
		CurrentReport: Comparison{
			CompareVersion: 1,
			Status:         CompareStatusConsistent,
			FromScan:       3,
			ToScan:         4,
			ComparedScans:  1,
		},
		ComparedScans: []ReviewScan{
			{Scan: 3, Status: reviewScanCompared, Baseline: &ScanRecord{Scan: 3}, Candidate: ScanRecord{Scan: 3}},
			{Scan: 4, Status: reviewScanMissing, Candidate: ScanRecord{Scan: 4}},
		},
	}
	controller, _ := NewController(demoProgram())
	manager := NewBranchManager(controller)
	pkg, err := manager.ImportReviewPackage(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.FormatVersion != reviewPackageVersion {
		t.Fatalf("旧格式应迁移到当前格式: %d", pkg.FormatVersion)
	}
	if len(pkg.ComparedScans) != 1 || len(pkg.MissingScans) != 1 || pkg.MissingScans[0].Baseline != nil {
		t.Fatalf("迁移后必须拆分实际比较与缺失扫描且不能补基线: %+v", pkg)
	}
	if pkg.CurrentReport.MissingScans == nil || pkg.HistoricalReports == nil || pkg.Notes == nil || pkg.BindingDescription == "" {
		t.Fatalf("迁移必须补齐空集合和绑定说明: %+v", pkg)
	}
}

func TestReviewPackageAllComparisonsMissingDoesNotFabricateBaseline(t *testing.T) {
	controller, _ := NewController(demoProgram())
	manager := NewBranchManager(controller)
	controller.Start()
	controller.Scan()
	controller.Scan()
	boundary := manager.SaveBoundary("")
	branch, _ := manager.CreateBranch(boundary.ID, "全部缺失")
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}, {}}); err != nil {
		t.Fatal(err)
	}

	pkg, err := manager.ExportReviewPackage(branch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.ComparedScans) != 0 || len(pkg.MissingScans) != 2 {
		t.Fatalf("全部缺失时应保存 0 个实际比较和 2 个缺失扫描: %+v", pkg)
	}
	for _, row := range pkg.MissingScans {
		if row.Baseline != nil || len(row.OutputDifferences) != 0 || len(row.InternalDifferences) != 0 {
			t.Fatalf("缺失扫描不能携带对照结论: %+v", row)
		}
	}
	if len(pkg.MissingIntervals) != 1 || pkg.MissingIntervals[0].FromScan != branch.BaseScan+1 || pkg.MissingIntervals[0].ToScan != branch.BaseScan+2 {
		t.Fatalf("全部缺失时仍应保留区间: %+v", pkg.MissingIntervals)
	}
}

func TestThreeLevelReviewPackageKeepsReportsAfterParentUpdateAbandonAndReload(t *testing.T) {
	controller, _ := NewController(demoProgram())
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	_ = controller.SetInput("start", false)
	for range 5 {
		controller.Scan()
	}
	boundary := manager.SaveBoundary("")
	level1, _ := manager.CreateBranch(boundary.ID, "一层")
	_, _ = manager.ReplaceSegment(level1.ID, level1.BaseScan+1, []map[string]bool{{}, {}, {}, {}})
	level2, _ := manager.ForkBranch(level1.ID, "二层", level1.BaseScan+2)
	_, _ = manager.ReplaceSegment(level2.ID, level2.BaseScan+1, []map[string]bool{{"stop": true}, {}})
	level3, _ := manager.ForkBranch(level2.ID, "三层", level2.BaseScan+1)
	_, _ = manager.ReplaceSegment(level3.ID, level3.BaseScan+1, []map[string]bool{{"start": true, "stop": false}})
	if _, err := manager.RebaseComparison(level3.ID); err != nil {
		t.Fatal(err)
	}
	pkg, err := manager.ExportReviewPackage(level3.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.CurrentReport.Divergence == nil {
		t.Fatalf("导出当前报告必须包含首次差异: %+v", pkg.CurrentReport)
	}
	if len(pkg.HistoricalReports) < 1 {
		t.Fatalf("导出包必须保留历史对照版本: %+v", pkg.HistoricalReports)
	}
	if len(pkg.ComparedScans) == 0 || len(pkg.MissingScans) != 0 {
		t.Fatalf("实际比较扫描应独立保存: %+v", pkg)
	}
	firstReport := pkg.HistoricalReports[len(pkg.HistoricalReports)-1]
	firstScan := firstReport.Divergence.Scan
	if firstReport.Divergence == nil || firstReport.Divergence.Scan != firstScan {
		t.Fatal("历史报告应保留其首次差异定位")
	}
	if _, err := manager.AddReviewNote(pkg.ID, firstReport.CompareVersion, firstScan, "复核首次输出差异与定时器状态"); err != nil {
		t.Fatal(err)
	}

	_, _ = manager.ReplaceSegment(level2.ID, level2.BaseScan+2, []map[string]bool{{"start": true}})
	if _, err := manager.AbandonBranch(level2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RebaseComparison(level3.ID); err != nil {
		t.Fatalf("后代仍应能依据放弃父分支的冻结记录重建对照: %v", err)
	}
	if pkg.CurrentReport.CompareVersion == level3.Comparison.CompareVersion {
		t.Fatal("父分支更新后的新对照不能回写已导出的旧审阅包")
	}

	path := filepath.Join(t.TempDir(), "store.json")
	if err := manager.Save(path); err != nil {
		t.Fatal(err)
	}
	otherController, _ := NewController(demoProgram())
	reloadedManager := NewBranchManager(otherController)
	if err := reloadedManager.Load(path); err != nil {
		t.Fatal(err)
	}
	reopened := reloadedManager.findReview(pkg.ID)
	if reopened == nil {
		t.Fatal("审阅包必须随分支存储重开")
	}
	if reopened.BranchStatus != branchStatusApplied || len(reopened.Notes) != 1 {
		t.Fatalf("重开后审阅说明丢失或分支状态错误: %+v", reopened)
	}
	gotFirst := false
	for _, report := range reopened.HistoricalReports {
		if report.CompareVersion == firstReport.CompareVersion && report.Divergence != nil && report.Divergence.Scan == firstScan {
			gotFirst = true
			if len(report.Divergence.Outputs) == 0 || report.Divergence.TimerState == nil {
				t.Fatalf("首次差异定位必须包含输出和定时器: %+v", report.Divergence)
			}
		}
	}
	if !gotFirst {
		t.Fatal("必须能按扫描序号定位历史报告的首次输出差异及定时器状态")
	}
}
