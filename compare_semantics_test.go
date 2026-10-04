package main

import (
	"path/filepath"
	"testing"
)

func latestReport(t *testing.T, branch *Branch) CompareReport {
	t.Helper()
	if len(branch.Reports) == 0 {
		t.Fatalf("分支 %s 应至少有一份对照报告", branch.ID)
	}
	return branch.Reports[len(branch.Reports)-1]
}

// 分支模拟了原执行尚未到达的未来扫描时，比较面板必须区分
// "已比较且一致" 与 "没有比较对象"，缺失记录不能当作已执行扫描。
func TestFutureScansAreNoBaselineNotIdentical(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	_ = controller.SetInput("start", false)
	controller.Scan()
	boundary := manager.SaveBoundary("")
	historyLen := len(controller.history)

	branch, err := manager.CreateBranch(boundary.ID, "未来扫描试验")
	if err != nil {
		t.Fatal(err)
	}
	// 原执行停在扫描 #2，分支模拟 #3、#4：两段都没有比较对象。
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}, {}}); err != nil {
		t.Fatal(err)
	}
	report := latestReport(t, branch)
	if report.Result != CompareResultNoBaseline {
		t.Fatalf("原执行未覆盖的扫描应判定为没有比较对象，得到 %s", report.Result)
	}
	if report.Compared != 0 || len(report.Missing) != 2 {
		t.Fatalf("应比较 0 段、缺失 2 段，得到 compared=%d missing=%v", report.Compared, report.Missing)
	}
	if branch.Divergence != nil {
		t.Fatal("没有比较对象时不能谎报差异")
	}

	// 原执行补扫一段后，部分扫描可比较且一致，其余仍是没有比较对象。
	controller.Scan()
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}, {}}); err != nil {
		t.Fatal(err)
	}
	report = latestReport(t, branch)
	if report.Result != CompareResultIdentical || report.Compared != 1 || len(report.Missing) != 1 {
		t.Fatalf("应已比较 1 段且一致、缺失 1 段，得到 %s compared=%d missing=%v",
			report.Result, report.Compared, report.Missing)
	}
	if report.Missing[0] != branch.BaseScan+2 {
		t.Fatalf("缺失的应是未来扫描 #%d，得到 %v", branch.BaseScan+2, report.Missing)
	}
	// 缺失对照区间的比较不得改写原执行历史。
	if len(controller.history) != historyLen+1 {
		t.Fatal("比较缺失对照区间不能改写原执行历史")
	}
}

// 父分支后续替换输入时，子分支的共同前缀与对照依据（快照）不能悄悄
// 变化；需要显式建立新的对照版本，同时保留旧报告。
func TestParentReplaceKeepsChildBaselineUntilRebaseline(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	_ = controller.SetInput("start", false)
	for range 3 {
		controller.Scan()
	}
	boundary := manager.SaveBoundary("")
	parent, _ := manager.CreateBranch(boundary.ID, "父分支")
	if _, err := manager.ReplaceSegment(parent.ID, parent.BaseScan+1, []map[string]bool{{}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	child, err := manager.ForkBranch(parent.ID, "子分支", parent.BaseScan+1)
	if err != nil {
		t.Fatal(err)
	}
	if child.BaseInputVersion != 1 || len(child.BaselineRecords) != 3 {
		t.Fatalf("子分支对照依据应固定为父分支输入版本 #1 的 3 段快照，得到 #%d/%d",
			child.BaseInputVersion, len(child.BaselineRecords))
	}
	reportsBefore := len(child.Reports)
	baselineBefore := cloneRecords(child.BaselineRecords)

	// 父分支替换输入（覆盖子分支派生点之后的扫描），子分支依据不变。
	if _, err := manager.ReplaceSegment(parent.ID, parent.BaseScan+2, []map[string]bool{{"stop": true}, {}}); err != nil {
		t.Fatal(err)
	}
	if parent.InputVersion != 2 {
		t.Fatal("父分支替换后应为输入版本 #2")
	}
	if len(child.Reports) != reportsBefore {
		t.Fatal("父分支替换不能悄悄改变子分支已有的对照报告")
	}
	if child.BaseInputVersion != 1 {
		t.Fatal("子分支对照依据仍应固定在输入版本 #1")
	}
	for i, record := range baselineBefore {
		if child.BaselineRecords[i].Inputs["stop"] != record.Inputs["stop"] {
			t.Fatal("子分支的对照快照不能随父分支替换而变化")
		}
	}
	if !manager.compareStale(child) {
		t.Fatal("父分支输入版本更新后，子分支对照依据应标记为已过期")
	}
	foundNote := false
	for _, note := range parent.OverlapNotes {
		if note != "" {
			foundNote = true
		}
	}
	if !foundNote || len(parent.OverlapNotes) == 0 {
		t.Fatal("父分支替换后应注明子分支仍固定在旧对照版本")
	}

	// 显式建立新的对照版本：快照更新、报告追加、旧报告保留。
	if _, err := manager.Rebaseline(child.ID); err != nil {
		t.Fatal(err)
	}
	if child.BaseInputVersion != 2 || len(child.Reports) != reportsBefore+1 {
		t.Fatalf("重建后应对照输入版本 #2 且报告追加，得到 #%d/%d 份",
			child.BaseInputVersion, len(child.Reports))
	}
	if manager.compareStale(child) {
		t.Fatal("重建对照版本后不应再标记过期")
	}
	latest := latestReport(t, child)
	if latest.Result != CompareResultDiverged || latest.Divergence == nil {
		t.Fatal("父分支加入停止脉冲后，新对照版本应报告首处真实输出差异")
	}
	if latest.Divergence.InternalState["runLatch"] != true {
		t.Fatal("差异位置必须报告当时内部保持状态 runLatch")
	}
	if _, ok := latest.Divergence.TimerState["runDelay"]; !ok {
		t.Fatal("差异位置必须报告当时定时器累计")
	}
	// 根分支对照原执行实时历史，无需重建。
	if _, err := manager.Rebaseline(parent.ID); err == nil {
		t.Fatal("根分支不应允许重建对照版本")
	}
}

// 三层分支：放弃中间层后转为只读保留记录，后代继续工作；
// 保存重开后对照报告、快照与派生关系保持一致。
func TestThreeLevelsAbandonMiddleAndReload(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	_ = controller.SetInput("start", false)
	for range 4 {
		controller.Scan()
	}
	boundary := manager.SaveBoundary("")
	level1, _ := manager.CreateBranch(boundary.ID, "一层")
	if _, err := manager.ReplaceSegment(level1.ID, level1.BaseScan+1, []map[string]bool{{}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	level2, _ := manager.ForkBranch(level1.ID, "二层", level1.BaseScan+1)
	if _, err := manager.ReplaceSegment(level2.ID, level2.BaseScan+1, []map[string]bool{{}, {}}); err != nil {
		t.Fatal(err)
	}
	level3, _ := manager.ForkBranch(level2.ID, "三层", level2.BaseScan+1)
	if _, err := manager.ReplaceSegment(level3.ID, level3.BaseScan+1, []map[string]bool{{"stop": true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AbandonBranch(level2.ID); err != nil {
		t.Fatalf("有后代的中间层应可放弃: %v", err)
	}
	if _, err := manager.ReplaceSegment(level2.ID, level2.BaseScan+1, []map[string]bool{{}}); err == nil {
		t.Fatal("放弃的中间层只读，不能再替换")
	}
	if _, err := manager.ForkBranch(level2.ID, "x", level2.BaseScan+1); err == nil {
		t.Fatal("放弃的中间层只读，不能再派生")
	}
	// 三层继续工作：对照依据是二层放弃前的快照。
	if _, err := manager.ReplaceSegment(level3.ID, level3.BaseScan+1, []map[string]bool{{}}); err != nil {
		t.Fatalf("中间层放弃后三层应可继续替换: %v", err)
	}
	if latestReport(t, level3).Result == "" {
		t.Fatal("三层应仍有有效对照报告")
	}

	path := filepath.Join(t.TempDir(), "store.json")
	if err := manager.Save(path); err != nil {
		t.Fatal(err)
	}
	reloaded := NewBranchManager(controller)
	if err := reloaded.Load(path); err != nil {
		t.Fatal(err)
	}
	var got2, got3 *Branch
	for i := range reloaded.branches {
		switch reloaded.branches[i].ID {
		case level2.ID:
			got2 = reloaded.branches[i]
		case level3.ID:
			got3 = reloaded.branches[i]
		}
	}
	if got2 == nil || got3 == nil {
		t.Fatal("重开后中间层与三层分支丢失")
	}
	if got2.Status != branchStatusAbandoned {
		t.Fatal("重开后中间层应仍为已放弃")
	}
	if len(got3.Reports) != len(level3.Reports) || len(got3.BaselineRecords) != len(level3.BaselineRecords) {
		t.Fatal("重开后对照报告与对照快照必须保留")
	}
	// 按程序修订与扫描序号找回只读历史（含已放弃中间层）。
	_, found := reloaded.FindTrials(controller.version, 0, level2.BaseScan+1)
	if len(found) < 3 {
		t.Fatalf("按版本与扫描序号应找回三层分支（含已放弃层），得到 %d", len(found))
	}
}

// 非法程序载入被拒绝且不改写原执行；缺失对照区间同样不改写。
func TestInvalidProgramAndMissingBaselineKeepOriginalExecution(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	for range 3 {
		controller.Scan()
	}
	versionBefore := controller.version
	historyLen := len(controller.history)
	scansBefore := controller.scans

	// 非法程序（存在反馈环路）载入必须失败且现场不变。
	bad := demoProgram()
	bad.Edges = append(bad.Edges, Edge{ID: "loop", Source: "motor", Target: "runLatch", SourcePort: "q", TargetPort: "set"})
	if err := controller.Load(bad); err == nil {
		t.Fatal("非法程序载入必须被拒绝")
	}
	if controller.version != versionBefore || controller.scans != scansBefore || len(controller.history) != historyLen {
		t.Fatal("非法程序载入不能改写原执行的版本、扫描计数与历史")
	}

	// 缺失对照区间的分支比较也不改写原执行。
	boundary := manager.SaveBoundary("")
	branch, _ := manager.CreateBranch(boundary.ID, "")
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{"stop": true}}); err != nil {
		t.Fatal(err)
	}
	if latestReport(t, branch).Result != CompareResultNoBaseline {
		t.Fatal("原执行未覆盖的扫描应判定为没有比较对象")
	}
	if len(controller.history) != historyLen || controller.scans != scansBefore {
		t.Fatal("缺失对照区间的比较不能改写原执行")
	}
}
