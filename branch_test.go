package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedLoadKeepsPausedProgram(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	_ = controller.SetInput("start", true)
	for range 3 {
		controller.Scan()
	}
	controller.Pause()
	version := controller.version
	scans := controller.scans
	elapsed := controller.timers["runDelay"].ElapsedMs

	delayed := demoProgram()
	for i := range delayed.Nodes {
		if delayed.Nodes[i].ID == "runDelay" {
			delayed.Nodes[i].PresetMs = 900
		}
	}
	if err := controller.LoadPreserve(delayed); err == nil {
		t.Fatal("不兼容变更必须被拒绝")
	}
	if controller.mode != "paused" || controller.version != version || controller.scans != scans {
		t.Fatal("载入失败后必须保留原暂停程序与状态，不能让用户误以为新版本已生效")
	}
	if controller.timers["runDelay"].ElapsedMs != elapsed || !controller.values["runLatch"] {
		t.Fatal("载入失败后定时累计与保持状态不能丢失")
	}
	if controller.program.program.Nodes[0].Label != "启动 PB1" {
		t.Fatal("载入失败后画布对应的程序必须保持原版本")
	}

	illegal := demoProgram()
	illegal.Edges = illegal.Edges[:1]
	if err := controller.Load(illegal); err == nil {
		t.Fatal("非法程序必须被拒绝")
	}
	if controller.mode != "paused" || controller.version != version || controller.scans != scans {
		t.Fatal("非法载入失败后原暂停程序必须保持不变")
	}
}

// 脉冲输入：分支在起点后替换一个扫描的虚拟输入，比较应定位首次差异。
func TestBranchPulseInputDivergence(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	for range 2 {
		controller.Scan()
	}
	boundary := controller.SaveBoundary("脉冲前")
	branch, err := controller.CreateBranch(boundary.ID, "启动脉冲")
	if err != nil {
		t.Fatal(err)
	}
	// 分支在扫描 3 注入一个启动脉冲（3 接通、4 断开），原执行保持断开。
	if err := controller.SetBranchOverrides(branch.ID, []InputOverride{
		{Scan: 3, Input: "start", Value: true},
		{Scan: 4, Input: "start", Value: false},
	}); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		controller.Scan()
	}
	if _, err := controller.ScanBranch(branch.ID, 4); err != nil {
		t.Fatal(err)
	}
	divergence, err := controller.CompareBranch(branch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !divergence.Found || divergence.Scan != 3 {
		t.Fatalf("脉冲应在扫描 3 首次产生差异: %+v", divergence)
	}
	if len(divergence.Outputs) == 0 || !strings.Contains(divergence.Outputs[0], "motor") {
		t.Fatalf("差异应涉及电机输出: %+v", divergence.Outputs)
	}
	foundLatch := false
	for _, item := range divergence.Internals {
		if strings.Contains(item, "runLatch") {
			foundLatch = true
		}
	}
	if !foundLatch {
		t.Fatalf("差异涉及的内部状态应包含运行保持: %+v", divergence.Internals)
	}
	// 分支不能修改原执行已经发生的输出。
	if controller.values["motor"] {
		t.Fatal("原执行在扫描 3 后电机仍应保持断开")
	}
	if controller.scans != 6 || branch.Scan != 6 {
		t.Fatal("原执行与分支的扫描序号应各自独立推进")
	}
}

// 互锁变化：原执行按下停止，分支保持原输入，比较定位互锁复位差异。
func TestBranchInterlockChangeDivergence(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	_ = controller.SetInput("start", false)
	for range 2 {
		controller.Scan()
	}
	boundary := controller.SaveBoundary("停止前")
	branch, err := controller.CreateBranch(boundary.ID, "无停止")
	if err != nil {
		t.Fatal(err)
	}
	_ = controller.SetInput("stop", true)
	controller.Scan()
	_ = controller.SetInput("stop", false)
	for range 2 {
		controller.Scan()
	}
	if _, err := controller.ScanBranch(branch.ID, 3); err != nil {
		t.Fatal(err)
	}
	divergence, err := controller.CompareBranch(branch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !divergence.Found || divergence.Scan != boundary.Scan+1 {
		t.Fatalf("停止互锁应在扫描 %d 首次产生差异: %+v", boundary.Scan+1, divergence)
	}
	if !branch.Values["runLatch"] {
		t.Fatal("分支未受停止互锁影响，运行保持应仍然置位")
	}
	if controller.values["runLatch"] {
		t.Fatal("原执行停止互锁后运行保持应复位")
	}
}

// 定时器跨分支起点保留累计依据；手动重新开始元件留下明确记录。
func TestBranchTimerBasisAndElementReset(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	_ = controller.SetInput("start", true)
	controller.Scan()
	_ = controller.SetInput("start", false)
	for range 2 {
		controller.Scan()
	}
	boundary := controller.SaveBoundary("计时中")
	branch, err := controller.CreateBranch(boundary.ID, "计时延续")
	if err != nil {
		t.Fatal(err)
	}
	if branch.Timers["runDelay"].ElapsedMs != 300 {
		t.Fatalf("分支应保留边界处的定时累计 300ms，实际 %d", branch.Timers["runDelay"].ElapsedMs)
	}
	if _, err := controller.ScanBranch(branch.ID, 2); err != nil {
		t.Fatal(err)
	}
	if !branch.Timers["runDelay"].Q {
		t.Fatal("跨起点保留累计后，分支再扫描 2 次应完成 500ms 延时")
	}
	if err := controller.ResetBranchElement(branch.ID, "runDelay"); err != nil {
		t.Fatal(err)
	}
	if branch.Timers["runDelay"].ElapsedMs != 0 || branch.Timers["runDelay"].Q {
		t.Fatal("重新开始元件后定时累计应清零")
	}
	if len(branch.ResetLog) != 1 || !strings.Contains(branch.ResetLog[0].Message, "runDelay") {
		t.Fatalf("重新开始元件必须留下明确记录: %+v", branch.ResetLog)
	}
	if branch.ResetLog[0].Scan != branch.Scan {
		t.Fatal("记录应携带操作发生时的分支扫描序号")
	}
	if err := controller.ResetBranchElement(branch.ID, "start"); err == nil {
		t.Fatal("非保持类元件不允许重新开始")
	}
}

// 共同前缀只读：覆盖不允许落在起点或已执行的扫描上。
func TestBranchOverrideReadOnlyPrefix(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	for range 2 {
		controller.Scan()
	}
	boundary := controller.SaveBoundary("前缀")
	branch, err := controller.CreateBranch(boundary.ID, "只读核对")
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.SetBranchOverrides(branch.ID, []InputOverride{
		{Scan: boundary.Scan, Input: "start", Value: true},
	}); err == nil {
		t.Fatal("共同前缀（起点及之前）必须只读")
	}
	if err := controller.SetBranchOverrides(branch.ID, []InputOverride{
		{Scan: boundary.Scan + 1, Input: "start", Value: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.ScanBranch(branch.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := controller.SetBranchOverrides(branch.ID, []InputOverride{
		{Scan: boundary.Scan + 1, Input: "start", Value: false},
	}); err == nil {
		t.Fatal("分支已执行的扫描不能改写")
	}
	if err := controller.SetBranchOverrides(branch.ID, []InputOverride{
		{Scan: boundary.Scan + 5, Input: "ghost", Value: true},
	}); err == nil {
		t.Fatal("不存在的输入必须被拒绝")
	}
}

// 修订后的元件删除 + 保存重开：各分支的输入、程序版本及扫描序号保持一致。
func TestBranchSurvivesRevisionAndReload(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	_ = controller.SetInput("start", true)
	for range 3 {
		controller.Scan()
	}
	boundary := controller.SaveBoundary("修订前")
	branch, err := controller.CreateBranch(boundary.ID, "旧版本分支")
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.SetBranchOverrides(branch.ID, []InputOverride{
		{Scan: 4, Input: "stop", Value: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.ScanBranch(branch.ID, 2); err != nil {
		t.Fatal(err)
	}

	// 主执行载入删除元件的修订版本（明确重新开始）。
	shrunk := demoProgram()
	shrunk.Nodes = shrunk.Nodes[:len(shrunk.Nodes)-1]
	shrunk.Edges = shrunk.Edges[:len(shrunk.Edges)-1]
	if err := controller.Load(shrunk); err != nil {
		t.Fatal(err)
	}
	if controller.version != 2 || controller.scans != 0 {
		t.Fatal("修订载入应递增版本并复位扫描计数")
	}
	// 分支仍固定在边界保存的程序版本，扫描序号不受主执行修订影响。
	if branch.Version != 1 || branch.Scan != 5 || len(branch.Program.Nodes) != 6 {
		t.Fatalf("分支应保持原程序版本与扫描序号: %+v", branch)
	}
	if _, err := controller.ScanBranch(branch.ID, 1); err != nil {
		t.Fatalf("修订删除元件后分支仍可按旧版本扫描: %v", err)
	}

	path := filepath.Join(t.TempDir(), "state.json")
	if err := controller.SaveToFile(path); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.branches) != 1 || len(restored.boundaries) != 1 {
		t.Fatal("保存重开后边界与分支必须保留")
	}
	restoredBranch := restored.branches[0]
	if restoredBranch.Scan != branch.Scan || restoredBranch.Version != branch.Version {
		t.Fatal("保存重开后分支的程序版本及扫描序号必须一致")
	}
	if restoredBranch.Inputs["start"] != branch.Inputs["start"] || restoredBranch.Inputs["stop"] != branch.Inputs["stop"] {
		t.Fatal("保存重开后分支的输入必须一致")
	}
	if len(restoredBranch.Overrides) != 1 || restoredBranch.Overrides[0].Scan != 4 {
		t.Fatal("保存重开后分支的输入覆盖必须一致")
	}
	if restored.version != controller.version || restored.scans != controller.scans {
		t.Fatal("保存重开后主执行的版本与扫描序号必须一致")
	}
	if _, err := restored.ScanBranch(restoredBranch.ID, 1); err != nil {
		t.Fatalf("恢复后的分支应可继续扫描: %v", err)
	}
	if restoredBranch.Timers["runDelay"].ElapsedMs != branch.Timers["runDelay"].ElapsedMs {
		t.Fatal("保存重开后分支的定时累计必须一致")
	}
}
