package main

import (
	"testing"
)

// 子分支从父分支派生：多轮替换、来源标注、与父分支比较，且不接回原执行。
func TestChildBranchForkOverlapAndSources(t *testing.T) {
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
	for range 4 {
		controller.Scan()
	}
	parent, err := manager.CreateBranch(boundary.ID, "父分支")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReplaceSegment(parent.ID, parent.BaseScan+1, []map[string]bool{
		{}, {}, {"stop": true}, {},
	}); err != nil {
		t.Fatal(err)
	}
	if parent.Status != branchStatusApplied || parent.InputVersion != 1 {
		t.Fatalf("替换后父分支应为 applied/版本1: %s/%d", parent.Status, parent.InputVersion)
	}

	forkAt := parent.BaseScan + 2
	child, err := manager.ForkBranch(parent.ID, "子分支", forkAt)
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID != parent.ID || child.BaseScan != forkAt {
		t.Fatal("子分支应记录父分支并冻结派生点")
	}
	// 继承父分支派生点之后的两段候选，来源必须标注为 parent（非真实扫描）。
	if len(child.Origins) != 2 {
		t.Fatalf("子分支应继承 2 段父分支候选，得到 %d", len(child.Origins))
	}
	for _, origin := range child.Origins {
		if origin.Source != "parent" || origin.SourceBranchID != parent.ID {
			t.Fatalf("继承段必须标注父分支来源: %+v", origin)
		}
	}
	if child.Status != branchStatusApplied {
		t.Fatal("继承候选并重新模拟后子分支应已有结果")
	}
	if child.Divergence != nil {
		t.Fatal("子分支刚继承与父分支完全相同的输入时，输出应与父分支一致，尚无差异")
	}

	// 在子分支上重叠替换：覆盖父分支的停止脉冲（扫描 #base+3）。
	if _, err := manager.ReplaceSegment(child.ID, forkAt+1, []map[string]bool{
		{}, {},
	}); err != nil {
		t.Fatal(err)
	}
	if child.InputVersion != 1 {
		t.Fatal("子分支首轮自己的替换应为输入版本 #1")
	}
	if len(child.OverlapNotes) == 0 {
		t.Fatal("与已模拟范围重叠时必须明确实际采用的输入及来源")
	}
	foundSelf := false
	for _, origin := range child.Origins {
		if origin.Scan == forkAt+1 && origin.Source == "self" {
			foundSelf = true
		}
	}
	if !foundSelf {
		t.Fatal("重叠替换后该扫描来源必须标注为本分支替换")
	}
	// 停止脉冲被去掉：子分支的 motor 在父分支停止后仍应保持。
	last := child.Records[len(child.Records)-1]
	if !last.Outputs["motor"] {
		t.Fatal("去掉停止脉冲后子分支电机应继续运行")
	}
	if child.Divergence == nil || child.Divergence.CompareTarget == "" {
		t.Fatal("子分支必须报告相对父分支的第一处真实输出差异")
	}
	if child.Divergence.InternalState["runLatch"] != true {
		t.Fatal("差异位置必须报告当时内部状态 runLatch")
	}
	// 父分支与原执行历史不被子分支修改。
	if parent.InputVersion != 1 {
		t.Fatal("子分支替换不能回写父分支")
	}
	motorStoppedInParent := false
	for _, record := range parent.Records {
		if record.Scan == child.Divergence.Scan && !record.Outputs["motor"] {
			motorStoppedInParent = true
		}
	}
	if !motorStoppedInParent {
		t.Fatal("父分支在停止脉冲后电机应已停止，作为子分支差异基线")
	}
}

// 三种状态：创建失败保留现场、尚无结果、已经生效。
func TestBranchStatusLifecycle(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	controller.Scan()
	boundary := manager.SaveBoundary("")
	before := len(manager.branches)
	if _, err := manager.CreateBranch("boundary-999", ""); err == nil {
		t.Fatal("不存在的边界必须创建失败")
	}
	if len(manager.branches) != before {
		t.Fatal("创建失败不能留下半成品分支，已有分支必须保留")
	}
	branch, err := manager.CreateBranch(boundary.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if branch.Status != branchStatusCreated || len(branch.Records) != 0 || len(branch.Segment) != 0 {
		t.Fatal("刚创建的分支应为尚无结果，且切片为空数组而非 null")
	}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}}); err != nil {
		t.Fatal(err)
	}
	if branch.Status != branchStatusApplied {
		t.Fatal("替换后应为已经生效")
	}
	if _, err := manager.AbandonBranch(branch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{"stop": true}}); err == nil {
		t.Fatal("放弃后的分支只读，不能再替换")
	}
	if len(branch.Records) == 0 {
		t.Fatal("放弃分支必须保留其扫描记录")
	}
	// 有未放弃子分支的中间分支也可以放弃：转为只读保留记录，
	// 子分支的对照依据已固定为快照，可继续替换与对照。
	controller2, _ := NewController(demoProgram())
	manager2 := NewBranchManager(controller2)
	controller2.Start()
	controller2.Scan()
	b2 := manager2.SaveBoundary("")
	p2, _ := manager2.CreateBranch(b2.ID, "p")
	_, _ = manager2.ReplaceSegment(p2.ID, p2.BaseScan+1, []map[string]bool{{}})
	c2, _ := manager2.ForkBranch(p2.ID, "", p2.BaseScan+1)
	if _, err := manager2.AbandonBranch(p2.ID); err != nil {
		t.Fatalf("中间分支有后代时也应可放弃（转为只读保留记录）: %v", err)
	}
	if p2.Status != branchStatusAbandoned || len(p2.Records) == 0 {
		t.Fatal("放弃后中间分支应为只读保留记录且记录不丢失")
	}
	if _, err := manager2.ReplaceSegment(p2.ID, p2.BaseScan+1, []map[string]bool{{"stop": true}}); err == nil {
		t.Fatal("放弃后的中间分支只读，不能再替换")
	}
	// 后代继续工作：子分支仍可替换输入并对照固定的快照依据。
	if _, err := manager2.ReplaceSegment(c2.ID, c2.BaseScan+1, []map[string]bool{{"stop": true}}); err != nil {
		t.Fatalf("父分支放弃后子分支应可继续替换: %v", err)
	}
	if c2.Status != branchStatusApplied || len(c2.Reports) == 0 {
		t.Fatal("父分支放弃后子分支应仍有生效的模拟结果与对照报告")
	}
}

// 持久化后可沿程序修订、输入版本与扫描序号找回，含派生关系与状态。
func TestPersistenceAndFindTrials(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	for range 3 {
		controller.Scan()
	}
	boundary := manager.SaveBoundary("")
	parent, _ := manager.CreateBranch(boundary.ID, "p")
	_, _ = manager.ReplaceSegment(parent.ID, parent.BaseScan+1, []map[string]bool{{}, {}})
	child, _ := manager.ForkBranch(parent.ID, "c", parent.BaseScan+1)
	_, _ = manager.ReplaceSegment(child.ID, child.BaseScan+1, []map[string]bool{{"stop": true}})
	path := t.TempDir() + "/store.json"
	if err := manager.Save(path); err != nil {
		t.Fatal(err)
	}
	reloaded := NewBranchManager(controller)
	if err := reloaded.Load(path); err != nil {
		t.Fatal(err)
	}
	_, got := reloaded.FindTrials(controller.version, 1, 0)
	if len(got) != 2 {
		t.Fatalf("程序版本+输入版本 #1 应找回父子各一次，得到 %d", len(got))
	}
	_, got = reloaded.FindTrials(0, 0, child.BaseScan)
	if len(got) < 2 {
		t.Fatal("按扫描序号应找回经过该序号的分支")
	}
	var restoredChild *Branch
	for i := range reloaded.branches {
		if reloaded.branches[i].ID == child.ID {
			restoredChild = reloaded.branches[i]
		}
	}
	if restoredChild == nil {
		t.Fatal("重开后子分支丢失")
	}
	if restoredChild.ParentID != parent.ID || restoredChild.ForkScan != child.ForkScan {
		t.Fatal("重开后派生关系与派生点必须保留")
	}
	if restoredChild.Status != branchStatusApplied || restoredChild.InputVersion != 1 {
		t.Fatal("重开后状态与输入版本必须保留")
	}
}
