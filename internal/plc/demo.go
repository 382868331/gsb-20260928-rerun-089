package plc

// DemoProgram is the start/stop interlock with seal-in latch and a delayed
// action:
//
//	Rung 1:  rail --|in.start|--+--|/in.stop|--(m.run)
//	                          |
//	                          +---| m.run |--+
//	Rung 2:  rail --| m.run |--[TON t.delay 1500ms]
//	Rung 3:  rail --| t.delay |------------(out.lamp)
//
// Pressing start energizes m.run; the m.run branch seals it in until stop
// opens. The TON accumulates only while m.run is powered and pausing the
// scanner freezes its elapsed time. After 1500 ms, out.lamp energizes.
func DemoProgram(version int64) *Program {
	n := func(id string, kind NodeKind, variable string, x, y float64, delay int64) Node {
		return Node{ID: id, Kind: kind, Variable: variable, DelayMs: delay, X: x, Y: y}
	}
	return &Program{
		Version: version,
		Name:    "start_stop_interlock_delay",
		Nodes: []Node{
			n("rail", NodeRail, "", 20, 150, 0),
			n("start", NodeContact, "in.start", 130, 80, 0),
			n("seal", NodeContact, "m.run", 130, 220, 0),
			n("or1", NodeOR, "", 290, 150, 0),
			n("stop", NodeNContact, "in.stop", 370, 150, 0),
			n("run", NodeCoil, "m.run", 510, 150, 0),
			n("run2", NodeContact, "m.run", 130, 350, 0),
			n("ton", NodeTON, "t.delay", 370, 350, 1500),
			n("dq", NodeContact, "t.delay", 130, 480, 0),
			n("lamp", NodeCoil, "out.lamp", 370, 480, 0),
		},
		Edges: []Edge{
			{ID: "e1", Source: "rail", Target: "start"},
			{ID: "e2", Source: "rail", Target: "seal"},
			{ID: "e3", Source: "start", Target: "or1"},
			{ID: "e4", Source: "seal", Target: "or1"},
			{ID: "e5", Source: "or1", Target: "stop"},
			{ID: "e6", Source: "stop", Target: "run"},
			{ID: "e7", Source: "rail", Target: "run2"},
			{ID: "e8", Source: "run2", Target: "ton"},
			{ID: "e9", Source: "rail", Target: "dq"},
			{ID: "e10", Source: "dq", Target: "lamp"},
		},
	}
}
