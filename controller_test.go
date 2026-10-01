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
	if err := controller.Load(demoProgram()); err != nil {
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

func renamedDemo(oldLabel, newLabel string) Program {
	program := demoProgram()
	for i := range program.Nodes {
		if program.Nodes[i].Label == oldLabel {
			program.Nodes[i].Label = newLabel
		}
	}
	return program
}

func TestPreviewReportsRevisionImpact(t *testing.T) {
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

	renamed := renamedDemo("启动 PB1", "启动按钮")
	diff, err := controller.Preview(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.Compatible || !diff.Paused || len(diff.RenamedInputs) != 1 {
		t.Fatalf("输入重命名应兼容并单独说明: %+v", diff)
	}

	delayed := demoProgram()
	for i := range delayed.Nodes {
		if delayed.Nodes[i].ID == "runDelay" {
			delayed.Nodes[i].PresetMs = 900
		}
	}
	diff, err = controller.Preview(delayed)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Compatible || len(diff.TimerChanges) != 1 {
		t.Fatalf("互锁延时修改应不兼容并单独说明: %+v", diff)
	}

	shrunk := demoProgram()
	shrunk.Nodes = shrunk.Nodes[:len(shrunk.Nodes)-1]
	shrunk.Edges = shrunk.Edges[:len(shrunk.Edges)-1]
	diff, err = controller.Preview(shrunk)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Compatible || len(diff.RemovedNodes) != 1 {
		t.Fatalf("元件删除应不兼容并单独说明: %+v", diff)
	}

	illegal := demoProgram()
	illegal.Edges = illegal.Edges[:1]
	if _, err := controller.Preview(illegal); err == nil {
		t.Fatal("非法连接必须在生效前被拒绝")
	}
}

func TestLoadPreserveKeepsCompatiblePausedState(t *testing.T) {
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
	elapsed := controller.timers["runDelay"].ElapsedMs
	scans := controller.scans
	version := controller.version

	if err := controller.LoadPreserve(renamedDemo("启动 PB1", "启动按钮")); err != nil {
		t.Fatal(err)
	}
	if controller.mode != "paused" {
		t.Fatal("暂停时保留载入后必须仍处于暂停，不能默默继续")
	}
	if controller.version != version+1 || controller.scans != scans {
		t.Fatal("保留载入应递增版本并保持扫描计数")
	}
	if controller.timers["runDelay"].ElapsedMs != elapsed || !controller.values["runLatch"] {
		t.Fatal("兼容载入不能清零定时累计或保持状态")
	}
	controller.Scan()
	if controller.timers["runDelay"].ElapsedMs != elapsed {
		t.Fatal("暂停期间扫描不能累计定时")
	}
	controller.Start()
	controller.Scan()
	if controller.timers["runDelay"].ElapsedMs != elapsed+scanIntervalMs {
		t.Fatal("恢复后应接着累计，不能重复或清零")
	}

	delayed := demoProgram()
	for i := range delayed.Nodes {
		if delayed.Nodes[i].ID == "runDelay" {
			delayed.Nodes[i].PresetMs = 900
		}
	}
	if err := controller.LoadPreserve(delayed); err == nil {
		t.Fatal("不兼容变更必须拒绝保留载入")
	}
	if err := controller.Load(delayed); err != nil {
		t.Fatal(err)
	}
	if controller.mode != "stopped" || controller.scans != 0 || controller.timers["runDelay"].ElapsedMs != 0 {
		t.Fatal("明确重新开始应复位运行态")
	}
}
