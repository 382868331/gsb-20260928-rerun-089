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
