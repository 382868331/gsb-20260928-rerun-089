package plc

import "testing"

func TestStartStopInterlockAndDelay(t *testing.T) {
	c := NewController(0)
	if errs := c.Load(DemoProgram(1)); len(errs) != 0 {
		t.Fatalf("demo should validate: %v", errs)
	}

	// First scan with start pressed: m.run latches via the seal-in branch.
	c.SetInput("in.start", true)
	r, ok := c.SingleStep()
	if !ok {
		t.Fatal("step returned no report")
	}
	if !r.Internals["m.run"] {
		t.Fatal("m.run should energize on start")
	}
	// Release the momentary start button; latch must hold across scans.
	c.SetInput("in.start", false)
	for i := 0; i < 5; i++ {
		r, _ = c.SingleStep()
	}
	if !r.Internals["m.run"] {
		t.Fatal("m.run must stay latched after start is released")
	}
	// Coils are committed after the logic phase, so m.run is visible to the
	// timer rung from scan 2: 6 scans => 500 ms accumulated.
	if r.Timers["t.delay"].ElapsedMs != 500 {
		t.Fatalf("timer should accumulate 500ms after 6 scans, got %d", r.Timers["t.delay"].ElapsedMs)
	}
	if r.Outputs["out.lamp"] {
		t.Fatal("lamp must stay off before delay completes")
	}

	// Eleven more scans cross 1500 ms (timer powered scans 2..16).
	for i := 0; i < 11; i++ {
		r, _ = c.SingleStep()
	}
	if !r.Outputs["out.lamp"] || !r.Timers["t.delay"].Q {
		t.Fatal("lamp should energize after 1500ms delay")
	}

	// Stop breaks the seal; coil commitment means effects propagate over
	// subsequent scans, as on a real controller.
	c.SetInput("in.stop", true)
	r, _ = c.SingleStep()
	// Stop remains held: the sealed latch must not re-close while it is open.
	r, _ = c.SingleStep()
	// Coil -> timer -> lamp effects propagate one scan each.
	r, _ = c.SingleStep()
	if r.Internals["m.run"] || r.Outputs["out.lamp"] {
		t.Fatal("stop must drop run and lamp")
	}
	if r.Timers["t.delay"].ElapsedMs != 0 {
		t.Fatalf("TON must reset when rung de-energizes, got %d", r.Timers["t.delay"].ElapsedMs)
	}
}

func TestPauseDoesNotResetTimer(t *testing.T) {
	c := NewController(0)
	c.Load(DemoProgram(1))
	c.SetInput("in.start", true)
	c.SingleStep()
	c.SetInput("in.start", false)
	c.SingleStep() // 100 ms accumulated (coil visible to timer from scan 2)
	c.Pause()
	before := c.Status().Report.Timers["t.delay"].ElapsedMs
	if before != 100 {
		t.Fatalf("expected 100ms before pause, got %d", before)
	}
	// No scans occur while paused: resume must continue the same accumulator.
	c.Run()
	c.Pause()
	after := c.Status().Report.Timers["t.delay"].ElapsedMs
	if after < before {
		t.Fatalf("resume restarted timing: before=%d after=%d", before, after)
	}
}

func TestInvalidProgramsRejected(t *testing.T) {
	cases := map[string]*Program{
		"cycle": {
			Version: 1,
			Nodes: []Node{
				{ID: "r", Kind: NodeRail},
				{ID: "a", Kind: NodeContact, Variable: "in.a"},
				{ID: "b", Kind: NodeOR},
				{ID: "q", Kind: NodeCoil, Variable: "out.q"},
			},
			Edges: []Edge{
				{ID: "w1", Source: "r", Target: "a"},
				{ID: "w2", Source: "a", Target: "b"},
				{ID: "w3", Source: "b", Target: "a"},
				{ID: "w4", Source: "b", Target: "q"},
			},
		},
		"coil in input namespace": {
			Version: 1,
			Nodes: []Node{
				{ID: "r", Kind: NodeRail},
				{ID: "q", Kind: NodeCoil, Variable: "in.q"},
			},
			Edges: []Edge{{ID: "w1", Source: "r", Target: "q"}},
		},
		"floating node": {
			Version: 1,
			Nodes: []Node{
				{ID: "r", Kind: NodeRail},
				{ID: "q", Kind: NodeCoil, Variable: "out.q"},
				{ID: "x", Kind: NodeCoil, Variable: "out.x"},
			},
			Edges: []Edge{{ID: "w1", Source: "r", Target: "q"}},
		},
	}
	for name, p := range cases {
		if errs, _ := p.Validate(); len(errs) == 0 {
			t.Fatalf("%s: expected validation errors", name)
		}
	}

	c := NewController(0)
	c.Load(DemoProgram(2))
	c.SetInput("in.start", true)
	c.SingleStep()
	if errs := c.Load(cases["cycle"]); len(errs) == 0 {
		t.Fatal("invalid program must not load")
	}
	st := c.Status()
	if st.ProgramVersion != 2 || !st.Report.Internals["m.run"] {
		t.Fatal("failed load must leave the running version and its state intact")
	}
}

func TestNewProgramVersionResetsState(t *testing.T) {
	c := NewController(0)
	c.Load(DemoProgram(1))
	c.SetInput("in.start", true)
	c.SingleStep()
	c.Load(DemoProgram(2))
	st := c.Status()
	if st.ProgramVersion != 2 || st.Report.Internals["m.run"] {
		t.Fatal("new version must load with fresh scan state")
	}
}
