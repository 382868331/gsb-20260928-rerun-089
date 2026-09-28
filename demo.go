package main

func demoProgram() Program {
	return Program{
		Name: "启动停止互锁与延时动作",
		Nodes: []Node{
			{ID: "start", Kind: "INPUT", Label: "启动 PB1", Position: map[string]int{"x": 40, "y": 40}},
			{ID: "stop", Kind: "INPUT", Label: "停止 PB2", Position: map[string]int{"x": 40, "y": 170}},
			{ID: "runLatch", Kind: "SR", Label: "运行保持(停止优先)", Position: map[string]int{"x": 270, "y": 80}},
			{ID: "runDelay", Kind: "TON", Label: "运行延时 500ms", PresetMs: 500, Position: map[string]int{"x": 520, "y": 80}},
			{ID: "motor", Kind: "OUTPUT", Label: "电机运行灯", Position: map[string]int{"x": 770, "y": 40}},
			{ID: "ready", Kind: "OUTPUT", Label: "延时就绪灯", Position: map[string]int{"x": 770, "y": 150}},
		},
		Edges: []Edge{
			{ID: "e-start-set", Source: "start", SourcePort: "q", Target: "runLatch", TargetPort: "set"},
			{ID: "e-stop-reset", Source: "stop", SourcePort: "q", Target: "runLatch", TargetPort: "reset"},
			{ID: "e-latch-motor", Source: "runLatch", SourcePort: "q", Target: "motor", TargetPort: "in"},
			{ID: "e-latch-timer", Source: "runLatch", SourcePort: "q", Target: "runDelay", TargetPort: "in"},
			{ID: "e-delay-ready", Source: "runDelay", SourcePort: "q", Target: "ready", TargetPort: "in"},
		},
	}
}
