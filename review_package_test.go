package main

import (
	"encoding/json"
	"testing"
)

func TestReviewPackageSnapshotsHistoryAcrossThreeLayersAndMissingRanges(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	if err := controller.SetInput("start", true); err != nil {
		t.Fatal(err)
	}
	controller.Scan()
	_ = controller.SetInput("start", false)
	controller.Scan()
	controller.Scan()
	boundary := manager.SaveBoundary("三层审阅边界")
	controller.Scan()

	parent, err := manager.CreateBranch(boundary.ID, "父分支")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReplaceSegment(parent.ID, parent.BaseScan+1, []map[string]bool{{}, {}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	if got := parent.Comparison.ComparedScanNumbers; len(got) != 1 || got[0] != 4 {
		t.Fatalf("父分支仅扫描 #4 有真实对照，得到 %v", got)
	}
	if len(parent.Comparison.MissingRanges) != 1 || parent.Comparison.MissingRanges[0] != (ScanRange{From: 5, To: 7}) {
		t.Fatalf("父分支应得到连续缺失区间 #5-#7：%+v", parent.Comparison.MissingRanges)
	}

	child, err := manager.ForkBranch(parent.ID, "子分支", 5)
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := manager.ForkBranch(child.ID, "孙分支", 6)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AddReviewNote(child.ID, 1, "旧父分支基线审阅说明"); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.ReplaceSegment(parent.ID, 5, []map[string]bool{{"stop": true}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	if child.Comparison.ParentInputVersion != 1 {
		t.Fatalf("父分支更新后子分支旧报告必须继续冻结输入版本 #1：%+v", child.Comparison)
	}
	if _, err := manager.RebaseComparison(child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AbandonBranch(parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AddReviewNote(child.ID, 2, "父分支更新后的新报告说明"); err != nil {
		t.Fatal(err)
	}

	parentPackage, err := manager.ExportReviewPackage(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parentPackage.CurrentReport.MissingRanges[0] != (ScanRange{From: 5, To: 7}) {
		t.Fatalf("放弃后导出的审阅包仍应保留缺失区间：%+v", parentPackage.CurrentReport.MissingRanges)
	}

	childPackage, err := manager.ExportReviewPackage(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(childPackage)
	if err != nil {
		t.Fatal(err)
	}
	var reopened ReviewPackage
	if err := json.Unmarshal(data, &reopened); err != nil {
		t.Fatal(err)
	}
	if len(reopened.ReviewNotes) != 2 {
		t.Fatalf("重新打开后应保留两个报告版本的说明：%+v", reopened.ReviewNotes)
	}
	if len(reopened.Reports) != 2 {
		t.Fatalf("审阅包应包含历史报告和当前报告，得到 %d", len(reopened.Reports))
	}
	var oldReport, currentReport *Comparison
	for i := range reopened.Reports {
		switch reopened.Reports[i].CompareVersion {
		case 1:
			oldReport = &reopened.Reports[i]
		case 2:
			currentReport = &reopened.Reports[i]
		}
	}
	if oldReport == nil || currentReport == nil {
		t.Fatal("重新打开后无法定位报告版本 #1/#2")
	}
	if oldReport.ParentInputVersion != 1 || len(oldReport.BaselineRecords) == 0 || len(oldReport.CandidateRecords) == 0 {
		t.Fatalf("旧报告必须冻结旧父输入版本及双侧扫描记录：%+v", oldReport)
	}
	oldBaseline := findRecord(oldReport.BaselineRecords, 6, child.BaseVersion)
	oldCandidate := findRecord(oldReport.CandidateRecords, 6, child.BaseVersion)
	newBaseline := findRecord(currentReport.BaselineRecords, 6, child.BaseVersion)
	if oldBaseline == nil || oldCandidate == nil || newBaseline == nil {
		t.Fatal("审阅包应能按扫描 #6 独立定位历史和当前定时器/输出状态")
	}
	if oldBaseline.Outputs["motor"] != true || oldCandidate.Outputs["motor"] != true || newBaseline.Outputs["motor"] != false {
		t.Fatalf("旧基线与新基线必须同时保留：oldBase=%t oldCandidate=%t newBase=%t",
			oldBaseline.Outputs["motor"], oldCandidate.Outputs["motor"], newBaseline.Outputs["motor"])
	}
	if currentReport.Divergence == nil || currentReport.Divergence.Scan != 6 {
		t.Fatalf("当前报告应定位到扫描 #6 的首次差异：%+v", currentReport.Divergence)
	}
	if currentReport.Divergence.TimerState["runDelay"].ElapsedMs == 0 {
		t.Fatal("首次差异位置应保留定时器累计状态")
	}

	grandPackage, err := manager.ExportReviewPackage(grandchild.ID)
	if err != nil {
		t.Fatal(err)
	}
	if grandPackage.CurrentReport.ParentInputVersion != 0 {
		t.Fatalf("孙分支报告应保留第二层冻结输入版本：%+v", grandPackage.CurrentReport)
	}
	grandBaseline := findRecord(grandPackage.CurrentReport.BaselineRecords, 7, grandchild.BaseVersion)
	if grandBaseline == nil || !grandBaseline.Outputs["motor"] {
		t.Fatalf("父分支更新和中间层放弃后，孙分支审阅包仍应能独立查看冻结基线：%+v", grandPackage.CurrentReport.BaselineRecords)
	}
}
