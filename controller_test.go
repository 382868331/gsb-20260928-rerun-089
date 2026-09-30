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
	if controller.version != oldVersion+1 || controller.scans != 0 || controller.mode != "running" {
		t.Fatal("运行中发布新版本应递增版本、清零扫描计数并保持运行模式")
	}
}

// customInterlockProgram 与内置示例不同：增加允许信号 iPermit，
// 通过 AND 门实现运行许可互锁，TON 预设 300ms，并增加一个独立的点动输出。
func customInterlockProgram() Program {
	return Program{
		Name: "许可互锁与短延时",
		Nodes: []Node{
			{ID: "iOn", Kind: "INPUT", Label: "合闸按钮", Position: map[string]int{"x": 20, "y": 20}},
			{ID: "iOff", Kind: "INPUT", Label: "分闸按钮", Position: map[string]int{"x": 20, "y": 140}},
			{ID: "iPermit", Kind: "INPUT", Label: "许可开关", Position: map[string]int{"x": 20, "y": 260}},
			{ID: "iJog", Kind: "INPUT", Label: "点动按钮", Position: map[string]int{"x": 20, "y": 380}},
			{ID: "latch", Kind: "SR", Label: "合闸保持", Position: map[string]int{"x": 260, "y": 80}},
			{ID: "gate", Kind: "AND", Label: "许可互锁", Position: map[string]int{"x": 480, "y": 80}},
			{ID: "delay", Kind: "TON", Label: "300ms 延时", PresetMs: 300, Position: map[string]int{"x": 700, "y": 80}},
			{ID: "motor", Kind: "OUTPUT", Label: "主接触器", Position: map[string]int{"x": 920, "y": 30}},
			{ID: "late", Kind: "OUTPUT", Label: "延时接通", Position: map[string]int{"x": 920, "y": 140}},
			{ID: "jogLamp", Kind: "OUTPUT", Label: "点动指示灯", Position: map[string]int{"x": 700, "y": 380}},
		},
		Edges: []Edge{
			{ID: "c1", Source: "iOn", SourcePort: "q", Target: "latch", TargetPort: "set"},
			{ID: "c2", Source: "iOff", SourcePort: "q", Target: "latch", TargetPort: "reset"},
			{ID: "c3", Source: "latch", SourcePort: "q", Target: "gate", TargetPort: "a"},
			{ID: "c4", Source: "iPermit", SourcePort: "q", Target: "gate", TargetPort: "b"},
			{ID: "c5", Source: "gate", SourcePort: "q", Target: "motor", TargetPort: "in"},
			{ID: "c6", Source: "gate", SourcePort: "q", Target: "delay", TargetPort: "in"},
			{ID: "c7", Source: "delay", SourcePort: "q", Target: "late", TargetPort: "in"},
			{ID: "c8", Source: "iJog", SourcePort: "q", Target: "jogLamp", TargetPort: "in"},
		},
	}
}

func TestCustomProgramPerScanInputsInternalsOutputs(t *testing.T) {
	controller, err := NewController(customInterlockProgram())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := controller.inputs["start"]; exists {
		t.Fatal("输入列表必须来自当前程序，不能残留示例的 start")
	}
	for _, id := range []string{"iOn", "iOff", "iPermit", "iJog"} {
		if _, exists := controller.inputs[id]; !exists {
			t.Fatalf("新程序输入 %s 必须可采样", id)
		}
	}
	controller.Start()

	// 无许可时合闸：SR 保持但 AND 互锁门关闭，电机不得输出。
	mustInput(t, controller, "iOn", true)
	controller.Scan()
	if !controller.values["latch"] || controller.values["gate"] || controller.values["motor"] {
		t.Fatal("许可缺失时互锁应阻止电机输出")
	}

	mustInput(t, controller, "iOn", false)
	mustInput(t, controller, "iPermit", true)
	controller.Scan() // 第 2 个有效扫描，延时 100ms
	state := controller.Scan()
	if !state.Outputs["motor"] || state.Timers["delay"].ElapsedMs != 200 || state.Timers["delay"].Q {
		t.Fatalf("互锁通过后电机应输出且延时累计 200ms：%+v", state.Timers["delay"])
	}
	if !state.Inputs["iPermit"] || state.Inputs["iOn"] {
		t.Fatal("状态中的输入列表必须反映新程序采样值")
	}

	controller.Scan() // 300ms 到点
	if !controller.values["late"] || !controller.timers["delay"].Q {
		t.Fatal("300ms 后延时输出应接通")
	}

	// 撤销许可：互锁门断开，TON 输入掉电立即清零。
	mustInput(t, controller, "iPermit", false)
	controller.Scan()
	if controller.values["motor"] || controller.values["late"] || controller.timers["delay"].ElapsedMs != 0 || controller.timers["delay"].Q {
		t.Fatal("许可断开时互锁应断开电机并复位通电延时")
	}

	// 点动输出不经过保持逻辑。
	mustInput(t, controller, "iJog", true)
	controller.Scan()
	if !controller.values["jogLamp"] {
		t.Fatal("点动输入应直接点亮指示灯")
	}
	mustInput(t, controller, "iJog", false)
	controller.Scan()
	if controller.values["jogLamp"] {
		t.Fatal("点动松开后指示灯应熄灭")
	}
}

func TestPublishVersionWhilePausedKeepsSameIDTimers(t *testing.T) {
	controller, err := NewController(customInterlockProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	mustInput(t, controller, "iOn", true)
	mustInput(t, controller, "iPermit", true)
	controller.Scan()
	controller.Scan() // delay 累计 200ms
	controller.Pause()
	pausedTimer := controller.timers["delay"]

	// 暂停时发布 v2：保留同 id TON，新增的 TON 从 0 开始，点动输出被删除。
	version2 := customInterlockProgram()
	version2.Nodes = append(version2.Nodes,
		Node{ID: "extraDelay", Kind: "TON", Label: "新增定时器", PresetMs: 1000, Position: map[string]int{"x": 700, "y": 480}},
		Node{ID: "extraLamp", Kind: "OUTPUT", Label: "新增定时灯", Position: map[string]int{"x": 920, "y": 480}},
	)
	version2.Edges = append(version2.Edges,
		Edge{ID: "c9", Source: "gate", SourcePort: "q", Target: "extraDelay", TargetPort: "in"},
		Edge{ID: "c10", Source: "extraDelay", SourcePort: "q", Target: "extraLamp", TargetPort: "in"},
	)

	if err := controller.Load(version2); err != nil {
		t.Fatal(err)
	}
	if controller.mode != "paused" || controller.version != 2 || controller.scans != 0 {
		t.Fatal("暂停发布新版本必须保持 paused 并清零扫描计数")
	}
	if controller.timers["delay"] != pausedTimer {
		t.Fatalf("同 id 的 TON 状态必须保留：got %+v want %+v", controller.timers["delay"], pausedTimer)
	}
	if controller.timers["extraDelay"].ElapsedMs != 0 || controller.timers["extraDelay"].PresetMs != 1000 {
		t.Fatal("新版本中新增的定时器必须从零开始")
	}
	if controller.values["latch"] {
		t.Fatal("发布新版本必须复位锁存等内部状态")
	}

	controller.Start()
	controller.Scan() // 恢复后第 1 个有效扫描：200ms + 100ms 到点
	if !controller.timers["delay"].Q || controller.timers["delay"].ElapsedMs != 300 {
		t.Fatalf("保留的定时器应从 200ms 继续到 300ms：%+v", controller.timers["delay"])
	}
}

func TestInvalidNewVersionDoesNotDisturbRunningProgram(t *testing.T) {
	controller, err := NewController(customInterlockProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	mustInput(t, controller, "iOn", true)
	mustInput(t, controller, "iPermit", true)
	controller.Scan()
	controller.Pause()
	oldVersion := controller.version
	oldTimer := controller.timers["delay"]

	illegal := customInterlockProgram()
	illegal.Edges = illegal.Edges[:3] // 必需端口未连接
	if err := controller.Load(illegal); err == nil {
		t.Fatal("非法程序必须返回错误")
	}
	if controller.mode != "paused" || controller.version != oldVersion {
		t.Fatal("非法新版本不得改变模式或版本号")
	}
	if controller.timers["delay"] != oldTimer {
		t.Fatal("非法新版本不得丢失旧定时器状态")
	}
	if _, exists := controller.program.nodes["extraDelay"]; exists {
		t.Fatal("非法程序绝不能部分载入")
	}
}

func mustInput(t *testing.T, controller *Controller, id string, value bool) {
	t.Helper()
	if err := controller.SetInput(id, value); err != nil {
		t.Fatal(err)
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
