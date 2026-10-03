package main

import (
	"path/filepath"
	"testing"
)

// 脉冲输入试验：原执行按一次脉冲，分支替换后续输入段后，
// 应报告首次输出差异的位置与涉及的内部状态，且共同前缀只读。
func TestBranchPulseInputDivergence(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	_ = controller.SetInput("start", false)
	for range 2 {
		controller.Scan()
	}
	boundary := manager.SaveBoundary("")
	// 原执行继续：保持输入断开，定时继续累计。
	for range 3 {
		controller.Scan()
	}
	historyLen := len(controller.history)

	branch, err := manager.CreateBranch(boundary.ID, "停止脉冲试验")
	if err != nil {
		t.Fatal(err)
	}
	if branch.BaseScan != boundary.Scan || branch.BaseVersion != controller.version {
		t.Fatal("分支应冻结边界的扫描序号与程序版本")
	}
	// 共同前缀只读：不允许从分支起点或更早替换。
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan, []map[string]bool{{"stop": true}}); err == nil {
		t.Fatal("共同前缀必须只读，不能从分支起点替换")
	}
	// 分支在边界后第一个扫描按下停止脉冲。
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{
		{"stop": true},
		{"stop": false},
	}); err != nil {
		t.Fatal(err)
	}
	if branch.Divergence == nil {
		t.Fatal("停止脉冲应使分支输出与原执行出现差异")
	}
	if branch.Divergence.Scan != branch.BaseScan+1 {
		t.Fatalf("首次差异应在扫描 #%d，得到 #%d", branch.BaseScan+1, branch.Divergence.Scan)
	}
	if len(branch.Divergence.Outputs) == 0 || branch.Divergence.Outputs[0] != "motor" {
		t.Fatalf("差异输出应包含 motor: %+v", branch.Divergence.Outputs)
	}
	foundLatch := false
	for _, id := range branch.Divergence.Internals {
		if id == "runLatch" {
			foundLatch = true
		}
	}
	if !foundLatch {
		t.Fatalf("涉及的内部状态应包含 runLatch: %+v", branch.Divergence.Internals)
	}
	// 分支不能修改原执行已经发生的输出。
	if len(controller.history) != historyLen {
		t.Fatal("分支操作不能修改原执行历史")
	}
	if !controller.values["motor"] {
		t.Fatal("分支模拟不能影响控制器当前状态")
	}
}

// 定时器跨分支起点时保留累计依据；用户明确重新开始某元件要留下记录。
func TestBranchTimerBasisAndRestartRecord(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	_ = controller.SetInput("start", false)
	for range 2 {
		controller.Scan()
	}
	elapsedAtBoundary := controller.timers["runDelay"].ElapsedMs
	boundary := manager.SaveBoundary("")
	branch, err := manager.CreateBranch(boundary.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if branch.BaseTimers["runDelay"].ElapsedMs != elapsedAtBoundary {
		t.Fatal("分支起点必须保留定时器累计依据")
	}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}, {}}); err != nil {
		t.Fatal(err)
	}
	last := branch.Records[len(branch.Records)-1]
	if last.Timers["runDelay"].ElapsedMs != elapsedAtBoundary+2*scanIntervalMs {
		t.Fatalf("分支定时器应从边界累计继续，得到 %dms", last.Timers["runDelay"].ElapsedMs)
	}
	if !last.Timers["runDelay"].Q {
		t.Fatal("累计达到 500ms 后分支内定时器应动作")
	}
	// 用户明确重新开始定时器：状态清零并留下记录。
	if _, err := manager.RestartElement(branch.ID, "runDelay"); err != nil {
		t.Fatal(err)
	}
	if len(branch.Restarts) != 1 {
		t.Fatal("重新开始元件必须留下明确记录")
	}
	if branch.BaseTimers["runDelay"].ElapsedMs != 0 {
		t.Fatal("重新开始后分支起点定时器应清零")
	}
	last = branch.Records[len(branch.Records)-1]
	if last.Timers["runDelay"].ElapsedMs != 2*scanIntervalMs || last.Timers["runDelay"].Q {
		t.Fatal("重新开始后分支定时器应从头累计")
	}
	if controller.timers["runDelay"].ElapsedMs != elapsedAtBoundary {
		t.Fatal("分支内重新开始不能影响原执行的定时器")
	}
	if _, err := manager.RestartElement(branch.ID, "motor"); err == nil {
		t.Fatal("OUTPUT 元件没有可重新开始的内部状态")
	}
}

// 修订删除元件后，旧边界建立的分支仍按原程序版本核对。
func TestBranchSurvivesRevisionWithRemovedNode(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	for range 2 {
		controller.Scan()
	}
	boundary := manager.SaveBoundary("")
	for range 2 {
		controller.Scan()
	}
	branch, err := manager.CreateBranch(boundary.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	// 修订：删除 ready 输出元件并明确重新开始。
	revised := demoProgram()
	revised.Nodes = revised.Nodes[:len(revised.Nodes)-1]
	revised.Edges = revised.Edges[:len(revised.Edges)-1]
	if err := controller.Load(revised); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{"stop": true}}); err != nil {
		t.Fatalf("修订删除元件后旧分支仍应可模拟: %v", err)
	}
	if branch.BaseVersion != boundary.Version || branch.BaseVersion == controller.version {
		t.Fatal("分支应保持建立时的程序版本")
	}
	foundReady := false
	for _, node := range branch.Program.Nodes {
		if node.ID == "ready" {
			foundReady = true
		}
	}
	if !foundReady {
		t.Fatal("分支程序应保留修订前的元件用于核对")
	}
	if branch.Divergence == nil {
		t.Fatal("分支替换输入后应与原执行历史出现差异")
	}
}

// 保存重开后各分支的输入段、程序版本及扫描序号保持一致。
func TestBranchPersistenceRoundTrip(t *testing.T) {
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
	boundary := manager.SaveBoundary("互锁边界")
	branch, err := manager.CreateBranch(boundary.ID, "脉冲分支")
	if err != nil {
		t.Fatal(err)
	}
	segment := []map[string]bool{{"start": false, "stop": true}, {"stop": false}}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, segment); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RestartElement(branch.ID, "runLatch"); err != nil {
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
	branches := reloaded.Branches()
	if len(branches) != 1 {
		t.Fatalf("重开后应恢复 1 个分支，得到 %d", len(branches))
	}
	got := branches[0]
	if got.BaseScan != branch.BaseScan || got.BaseVersion != branch.BaseVersion {
		t.Fatal("重开后分支的扫描序号与程序版本必须一致")
	}
	if got.ProgramName != branch.ProgramName || len(got.Program.Nodes) != len(branch.Program.Nodes) {
		t.Fatal("重开后分支程序必须一致")
	}
	if len(got.Segment) != len(segment) || got.Segment[0]["stop"] != true {
		t.Fatal("重开后分支替换输入段必须一致")
	}
	if len(got.Records) != len(branch.Records) || got.Records[0].Scan != branch.BaseScan+1 {
		t.Fatal("重开后分支记录扫描序号必须一致")
	}
	if len(got.Restarts) != 1 {
		t.Fatal("重开后重新开始的记录必须保留")
	}
	if len(reloaded.Boundaries()) != 1 || reloaded.Boundaries()[0].Label != "互锁边界" {
		t.Fatal("重开后边界必须保留")
	}
	// 重开后继续替换输入段，id 与序号不能冲突。
	again, err := reloaded.CreateBranch(reloaded.Boundaries()[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID == got.ID {
		t.Fatal("重开后新建分支 id 不能冲突")
	}
}

func TestChildBranchUsesOnlyExecutedRecordsAndOverridesOverlap(t *testing.T) {
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
	parent, err := manager.CreateBranch(boundary.ID, "父分支")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReplaceSegment(parent.ID, parent.BaseScan+1, []map[string]bool{{}, {}}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreateChildBranch(parent.ID, parent.BaseScan+5, "子分支"); err == nil {
		t.Fatal("不能从尚未执行的候选扫描派生子分支")
	}
	child, err := manager.CreateChildBranch(parent.ID, parent.BaseScan+1, "子分支")
	if err != nil {
		t.Fatal(err)
	}
	if len(child.InputPlan) != 1 || child.InputPlan[0].Scan != parent.BaseScan+2 {
		t.Fatalf("子分支应只继承起点之后的父分支计划：%+v", child.InputPlan)
	}
	if _, err := manager.ReplaceSegment(child.ID, child.BaseScan+1, []map[string]bool{{"stop": true}}); err != nil {
		t.Fatal(err)
	}
	frame := child.InputPlan[0]
	if !frame.Inputs["stop"] {
		t.Fatal("重叠范围应采用子分支的新输入")
	}
	if frame.Sources["stop"] != "子分支 输入版本 1" {
		t.Fatalf("重叠输入必须标明来自本次子分支版本：%s", frame.Sources["stop"])
	}
	if child.Divergence == nil || child.Divergence.BaselineID != parent.ID {
		t.Fatal("子分支必须与父分支真实记录比较")
	}
	if child.Divergence.Scan != child.BaseScan+1 {
		t.Fatalf("首次差异位置错误：%+v", child.Divergence)
	}
}

func TestFailedReplacementKeepsPreviousBranchResult(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	boundary := manager.SaveBoundary("")
	branch, err := manager.CreateBranch(boundary.ID, "失败回滚")
	if err != nil {
		t.Fatal(err)
	}
	good := []map[string]bool{{}, {}}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, good); err != nil {
		t.Fatal(err)
	}
	oldRecords := len(branch.Records)
	oldVersion := branch.InputVersion
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{"missing": true}}); err == nil {
		t.Fatal("不存在的输入必须拒绝")
	}
	if len(branch.Records) != oldRecords || branch.InputVersion != oldVersion || branch.LastError == "" {
		t.Fatal("失败替换必须保留已有输入版本、扫描记录并记录错误")
	}
}

func TestBranchArchiveFindsOriginalAfterProgramRevision(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	boundary := manager.SaveBoundary("")
	for range 2 {
		controller.Scan()
	}
	branch, err := manager.CreateBranch(boundary.ID, "修订后比较")
	if err != nil {
		t.Fatal(err)
	}
	revised := demoProgram()
	revised.Nodes = revised.Nodes[:len(revised.Nodes)-1]
	revised.Edges = revised.Edges[:len(revised.Edges)-1]
	if err := controller.Load(revised); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{"stop": true}}); err != nil {
		t.Fatal(err)
	}
	if branch.Divergence == nil {
		t.Fatal("程序修订后仍应从原始扫描档案找到比较基准")
	}
}
