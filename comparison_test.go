package main

import (
	"path/filepath"
	"testing"
)

// 分支模拟了原执行尚未到达的未来扫描时，比较面板必须明确
// “没有比较对象”，不能把缺失记录当作已比较且输出一致。
func TestComparisonDistinguishesMissingFromConsistent(t *testing.T) {
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
	// 原执行停在边界处，分支模拟了 3 个未来扫描：没有任何对照记录。
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	if branch.Divergence != nil {
		t.Fatal("没有对照记录时不能报告差异")
	}
	if branch.Comparison == nil {
		t.Fatal("必须给出明确的对照结论")
	}
	if branch.Comparison.Status != CompareStatusMissing {
		t.Fatalf("原执行没有对应记录时必须是没有比较对象，得到 %s", branch.Comparison.Status)
	}
	if branch.Comparison.ComparedScans != 0 || len(branch.Comparison.MissingScans) != 3 {
		t.Fatalf("缺失扫描必须逐条列出: %+v", branch.Comparison)
	}
	// 缺失对照区间不能改写原执行。
	if len(controller.history) != historyLen {
		t.Fatal("缺失对照区间的比较不能改写原执行历史")
	}

	// 原执行补上 2 个扫描后重新替换：部分可比较、部分仍缺失。
	for range 2 {
		controller.Scan()
	}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	if branch.Comparison.Status != CompareStatusConsistent {
		t.Fatalf("可比较部分输出一致时应为确实一致，得到 %s", branch.Comparison.Status)
	}
	if branch.Comparison.ComparedScans != 2 || len(branch.Comparison.MissingScans) != 1 {
		t.Fatalf("应比较 2 个扫描并标出 1 个缺失扫描: %+v", branch.Comparison)
	}
	if branch.Comparison.ProgramVersion != branch.BaseVersion ||
		branch.Comparison.FromScan != branch.BaseScan+1 {
		t.Fatal("对照结论必须固定程序修订与扫描范围")
	}
}

// 父分支后续替换输入时，已有子分支的共同前缀与比较依据不能悄悄变化；
// 必须显式建立新的对照版本，旧报告保留。
func TestParentReplaceKeepsChildBaselineUntilRebase(t *testing.T) {
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
	parent, _ := manager.CreateBranch(boundary.ID, "父分支")
	if _, err := manager.ReplaceSegment(parent.ID, parent.BaseScan+1, []map[string]bool{{}, {}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	forkAt := parent.BaseScan + 2
	child, err := manager.ForkBranch(parent.ID, "子分支", forkAt)
	if err != nil {
		t.Fatal(err)
	}
	if child.BaselineInputVersion != parent.InputVersion || len(child.Baseline) != len(parent.Records) {
		t.Fatal("子分支必须冻结派生时父分支的对照快照")
	}
	frozenComparison := child.Comparison

	// 父分支替换派生点之后的输入：子分支对照依据不能悄悄变化。
	if _, err := manager.ReplaceSegment(parent.ID, forkAt+1, []map[string]bool{{"stop": true}, {}}); err != nil {
		t.Fatal(err)
	}
	if child.BaselineInputVersion == parent.InputVersion {
		t.Fatal("父分支替换输入后，子分支的冻结对照不能跟着变")
	}
	if child.Comparison.CompareVersion != frozenComparison.CompareVersion ||
		child.Comparison.Status != frozenComparison.Status {
		t.Fatal("父分支替换输入不能悄悄改变已有子分支的对照结论")
	}
	noted := false
	for _, note := range parent.OverlapNotes {
		if note != "" {
			noted = true
		}
	}
	if !noted {
		t.Fatal("父分支替换输入时必须提示子分支对照仍冻结在旧版本")
	}

	// 显式建立新的对照版本：旧报告保留，新对照基于父分支当前记录。
	if _, err := manager.RebaseComparison(child.ID); err != nil {
		t.Fatal(err)
	}
	if child.CompareVersion != 2 || child.BaselineInputVersion != parent.InputVersion {
		t.Fatal("重建对照后应固定到父分支最新输入版本")
	}
	if len(child.Reports) != 1 || child.Reports[0].CompareVersion != 1 {
		t.Fatal("旧对照报告必须只读保留")
	}
	if child.Comparison.Status != CompareStatusDiverged || child.Comparison.Divergence == nil {
		t.Fatal("新对照应报告与父分支新记录的第一处真实差异")
	}
	if child.Comparison.Divergence.InternalState["runLatch"] != true {
		t.Fatal("差异位置必须报告当时内部保持状态")
	}
	// 再次重建：旧报告继续累积。
	if _, err := manager.RebaseComparison(child.ID); err != nil {
		t.Fatal(err)
	}
	if len(child.Reports) != 2 {
		t.Fatal("每次重建对照都必须保留旧报告")
	}
	// 顶层分支不需要重建对照。
	if _, err := manager.RebaseComparison(parent.ID); err == nil {
		t.Fatal("顶层分支直接对照原执行历史，无需重建")
	}
}

// 三层分支：放弃中间层后，后代继续工作；保存重开后对照语义保持。
func TestThreeLevelBranchAbandonMiddleAndReload(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
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
	if _, err := manager.ReplaceSegment(level1.ID, level1.BaseScan+1, []map[string]bool{{}, {}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	level2, err := manager.ForkBranch(level1.ID, "二层", level1.BaseScan+2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReplaceSegment(level2.ID, level2.BaseScan+1, []map[string]bool{{"stop": true}, {}}); err != nil {
		t.Fatal(err)
	}
	level3, err := manager.ForkBranch(level2.ID, "三层", level2.BaseScan+1)
	if err != nil {
		t.Fatal(err)
	}
	if level3.BaselineInputVersion != level2.InputVersion {
		t.Fatal("三层分支必须冻结二层派生时的输入版本")
	}

	// 放弃中间层：二层转为只读保留，三层继续工作。
	if _, err := manager.AbandonBranch(level2.ID); err != nil {
		t.Fatalf("中间层应可放弃: %v", err)
	}
	if _, err := manager.ReplaceSegment(level2.ID, level2.BaseScan+1, []map[string]bool{{}}); err == nil {
		t.Fatal("放弃的中间层不能再编辑")
	}
	if _, err := manager.ForkBranch(level2.ID, "四层", level2.BaseScan+1); err == nil {
		t.Fatal("放弃的中间层不能再派生")
	}
	if _, err := manager.ReplaceSegment(level3.ID, level3.BaseScan+1, []map[string]bool{{"start": true}}); err != nil {
		t.Fatalf("中间层放弃后三层分支应继续工作: %v", err)
	}
	if level3.Comparison == nil || level3.Comparison.Status == "" {
		t.Fatal("三层分支必须给出明确对照结论")
	}
	// 原执行历史不被任何分支操作改写。
	if len(controller.history) != 6 {
		t.Fatal("分支操作不能改写原执行历史")
	}

	// 保存重开：三层分支的冻结对照、报告与状态保持一致。
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
		t.Fatal("重开后二三层分支都必须找回")
	}
	if got2.Status != branchStatusAbandoned {
		t.Fatal("重开后中间层必须保持只读保留状态")
	}
	if got3.BaselineInputVersion != level3.BaselineInputVersion ||
		got3.Comparison == nil || got3.Comparison.Status != level3.Comparison.Status {
		t.Fatal("重开后三层分支的冻结对照与结论必须一致")
	}
	// 只读历史可按程序修订与扫描序号找回。
	_, trials := reloaded.FindTrials(controller.version, 0, level3.BaseScan+1)
	if len(trials) == 0 {
		t.Fatal("只读历史必须可按程序修订与扫描序号找回")
	}
}

// 对照结论随分支持久化：旧报告、缺失扫描列表重开后不丢失。
func TestComparisonReportsPersistAcrossReload(t *testing.T) {
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
	parent, _ := manager.CreateBranch(boundary.ID, "p")
	if _, err := manager.ReplaceSegment(parent.ID, parent.BaseScan+1, []map[string]bool{{}, {}, {}}); err != nil {
		t.Fatal(err)
	}
	child, _ := manager.ForkBranch(parent.ID, "c", parent.BaseScan+1)
	if _, err := manager.ReplaceSegment(parent.ID, parent.BaseScan+2, []map[string]bool{{"stop": true}, {}}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RebaseComparison(child.ID); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "store.json")
	if err := manager.Save(path); err != nil {
		t.Fatal(err)
	}
	reloaded := NewBranchManager(controller)
	if err := reloaded.Load(path); err != nil {
		t.Fatal(err)
	}
	var got *Branch
	for i := range reloaded.branches {
		if reloaded.branches[i].ID == child.ID {
			got = reloaded.branches[i]
		}
	}
	if got == nil {
		t.Fatal("重开后子分支丢失")
	}
	if got.CompareVersion != 2 || len(got.Reports) != 1 {
		t.Fatal("重开后对照版本与旧报告必须保留")
	}
	if got.Comparison == nil || got.Comparison.ParentInputVersion != parent.InputVersion {
		t.Fatal("重开后对照必须仍固定父分支输入版本")
	}
	if got.Comparison.MissingScans == nil || got.Baseline == nil || got.Reports[0].MissingScans == nil {
		t.Fatal("序列化字段必须为空数组而不是 null")
	}
}
