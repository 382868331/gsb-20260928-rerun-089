package main

import "testing"

func TestInterlockDelayPauseAndNewVersion(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	if err := controller.SetInput("start", true); err != nil {
		t.Fatal(err)
	}
	controller.Scan()
	if !controller.values["motor"] {
		t.Fatal("启动一次扫描后电机应通过 SR 保持")
	}
	if err := controller.SetInput("start", false); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		controller.Scan()
	}
	if !controller.values["motor"] || controller.timers["runDelay"].Q {
		t.Fatal("400ms 时电机保持，延时输出尚未到达")
	}
	controller.Pause()
	before := controller.timers["runDelay"]
	controller.Scan()
	if controller.timers["runDelay"] != before {
		t.Fatal("暂停期间不能继续计时或复位")
	}
	controller.Start()
	controller.Scan()
	if !controller.timers["runDelay"].Q || !controller.values["ready"] {
		t.Fatal("恢复后的第 5 个有效扫描应完成 500ms 延时")
	}
	if err := controller.SetInput("stop", true); err != nil {
		t.Fatal(err)
	}
	controller.Scan()
	if controller.values["motor"] || controller.values["ready"] || controller.timers["runDelay"].ElapsedMs != 0 {
		t.Fatal("停止优先互锁应复位输出和定时器")
	}
	oldVersion := controller.version
	if err := controller.Load(demoProgram(), false); err != nil {
		t.Fatal(err)
	}
	if controller.version != oldVersion+1 || controller.scans != 0 || controller.mode != "stopped" {
		t.Fatal("编辑后程序应作为新版本载入并复位运行态")
	}
}

func TestRejectsIllegalConnections(t *testing.T) {
	valid := demoProgram()
	tests := []struct {
		name   string
		mutate func(*Program)
	}{
		{name: "missing connection", mutate: func(program *Program) { program.Edges = program.Edges[:1] }},
		{name: "feedback loop", mutate: func(program *Program) {
			program.Edges[0] = Edge{ID: "loop", Source: "runLatch", SourcePort: "q", Target: "runLatch", TargetPort: "set"}
		}},
		{name: "output as source", mutate: func(program *Program) {
			program.Edges[0] = Edge{ID: "bad", Source: "motor", SourcePort: "q", Target: "runLatch", TargetPort: "set"}
		}},
		{name: "duplicate input", mutate: func(program *Program) {
			program.Edges[0].TargetPort = "reset"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program := valid
			test.mutate(&program)
			if _, err := compileProgram(program); err == nil {
			t.Fatal("非法连接程序不能运行")
			}
		})
	}
}

func TestPreviewAndMergeKeepCompatibleState(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	if err := controller.SetInput("start", true); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		controller.Scan()
	}
	controller.Pause()
	elapsed := controller.timers["runDelay"].ElapsedMs
	if elapsed == 0 {
		t.Fatal("暂停前定时器应有累计值")
	}

	revised := demoProgram()
	revised.Nodes[1].Label = "急停 PB2"
	revised.Nodes[3].PresetMs = 800
	revised.Nodes = revised.Nodes[:5]
	revised.Edges = revised.Edges[:4]

	impact, err := controller.Preview(revised)
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.TimerChanges) != 1 || impact.TimerChanges[0].ID != "runDelay" {
		t.Fatal("预览应报告互锁延时变化")
	}
	if len(impact.RenamedInputs) != 1 || impact.RenamedInputs[0].ID != "stop" {
		t.Fatal("预览应报告输入重命名")
	}
	if len(impact.RemovedNodes) != 1 || impact.RemovedNodes[0].ID != "ready" {
		t.Fatal("预览应报告被删除的元件")
	}

	if _, err := controller.Preview(demoProgram()); err != nil {
		t.Fatal(err)
	}
	illegal := demoProgram()
	illegal.Edges = illegal.Edges[:1]
	if _, err := controller.Preview(illegal); err == nil {
		t.Fatal("非法连接在预览阶段就应被拒绝")
	}

	oldVersion := controller.version
	if err := controller.Load(revised, true); err != nil {
		t.Fatal(err)
	}
	if controller.version != oldVersion+1 {
		t.Fatal("保留兼容状态也应递增版本")
	}
	if controller.mode != "paused" {
		t.Fatal("保留兼容状态时暂停状态不能改变")
	}
	if controller.timers["runDelay"].ElapsedMs != 0 {
		t.Fatal("互锁延时变化后该定时器累计应清零")
	}
	if !controller.inputs["start"] || !controller.values["runLatch"] {
		t.Fatal("未受影响的输入采样与保持状态应保留")
	}
	if _, ok := controller.values["ready"]; ok {
		t.Fatal("被删除元件的状态应移除")
	}

	controller.Start()
	for range 2 {
		controller.Scan()
	}
	accumulated := controller.timers["runDelay"].ElapsedMs
	if accumulated != 200 {
		t.Fatalf("恢复后应累计 200ms，得到 %d", accumulated)
	}
	if err := controller.Load(revised, true); err != nil {
		t.Fatal(err)
	}
	if controller.timers["runDelay"].ElapsedMs != accumulated {
		t.Fatal("同预设保留载入不能清零或重复累计定时器")
	}
	if controller.scans == 0 {
		t.Fatal("保留兼容状态时应延续同一次执行的扫描计数")
	}
}
