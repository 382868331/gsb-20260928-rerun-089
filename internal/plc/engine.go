package plc

import (
	"strings"
	"sync"
)

// Engine executes one validated program version. Scan state (latched
// internals/outputs and timer accumulators) is owned by the engine and carried
// across scans. Pausing the scheduler never resets it.
type Engine struct {
	mu         sync.Mutex
	program    *Program
	nodes      map[string]Node
	indeg      map[string]int
	outs       map[string][]string
	ins        map[string][]string
	inputs     map[string]bool
	intern     map[string]bool
	outputs    map[string]bool
	timers     map[string]*TimerState
	readTimers map[string]TimerState
	scan       uint64
	running    bool
}

// Load validates the program first. On success it becomes a new version:
// execution switches atomically and scan state starts fresh for the new
// version. Invalid programs are rejected and the running version is untouched.
func Load(p *Program) (*Engine, []ValidateError) {
	if p == nil {
		return nil, []ValidateError{{Message: "program is empty"}}
	}
	errs, nodes := p.Validate()
	if len(errs) > 0 {
		return nil, errs
	}
	e := &Engine{
		program: p,
		nodes:   nodes,
		indeg:   map[string]int{},
		outs:    map[string][]string{},
		ins:     map[string][]string{},
		inputs:  map[string]bool{},
		intern:  map[string]bool{},
		outputs: map[string]bool{},
		timers:  map[string]*TimerState{},
	}
	for _, edge := range p.Edges {
		e.outs[edge.Source] = append(e.outs[edge.Source], edge.Target)
		e.ins[edge.Target] = append(e.ins[edge.Target], edge.Source)
		e.indeg[edge.Target]++
	}
	for _, n := range nodes {
		switch n.Kind {
		case NodeTON:
			e.timers[n.Variable] = &TimerState{}
		case NodeCoil:
			if strings.HasPrefix(n.Variable, "out.") {
				e.outputs[n.Variable] = false
			} else {
				e.intern[n.Variable] = false
			}
		}
	}
	// Default-declare contact input variables so observers see them.
	for _, n := range nodes {
		if (n.Kind == NodeContact || n.Kind == NodeNContact) && strings.HasPrefix(n.Variable, "in.") {
			if _, ok := e.inputs[n.Variable]; !ok {
				e.inputs[n.Variable] = false
			}
		}
	}
	return e, nil
}

func (e *Engine) Program() Program {
	e.mu.Lock()
	defer e.mu.Unlock()
	return *e.program
}

func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

func (e *Engine) SetRunning(v bool) {
	e.mu.Lock()
	e.running = v
	e.mu.Unlock()
}

// SetInput updates a physical/input table value, sampled at scan start.
func (e *Engine) SetInput(name string, value bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !strings.HasPrefix(name, "in.") {
		return false
	}
	e.inputs[name] = value
	return true
}

// valueOf reads a variable from the previous-scan committed tables, never from
// values half-written during the current scan.
func (e *Engine) valueOf(name string) bool {
	switch {
	case strings.HasPrefix(name, "in."):
		return e.inputs[name]
	case strings.HasPrefix(name, "out."):
		return e.outputs[name]
	case strings.HasPrefix(name, "m."):
		return e.intern[name]
	case strings.HasPrefix(name, "t."):
		if e.readTimers != nil {
			return e.readTimers[name].Q
		}
		if t, ok := e.timers[name]; ok {
			return t.Q
		}
	}
	return false
}

// Scan runs one deterministic cycle with a fixed scan period and returns the
// committed report. dtMs is nominal elapsed scan time for timer accumulation;
// while paused the scheduler simply does not call Scan, so timers never reset.
func (e *Engine) Scan(dtMs int64) ScanReport {
	e.mu.Lock()
	defer e.mu.Unlock()

	power := map[string]bool{}
	nextIntern := map[string]bool{}
	nextOutput := map[string]bool{}
	// Every coil defaults off on each scan; only powered coils are set during
	// the output phase. Old latched values live in e.intern/e.outputs for
	// contacts to read, not in the table being built.
	for k := range e.intern {
		nextIntern[k] = false
	}
	for k := range e.outputs {
		nextOutput[k] = false
	}

	// Kahn topological evaluation; validation already guaranteed acyclicity.
	indeg := map[string]int{}
	for k, v := range e.indeg {
		indeg[k] = v
	}
	var railID string
	for id, n := range e.nodes {
		if n.Kind == NodeRail {
			railID = id
		}
	}
	queue := []string{railID}
	pendingCoils := map[string]bool{}
	pendingTimers := map[string]TimerState{}
	readTimers := map[string]TimerState{}
	for k, v := range e.timers {
		readTimers[k] = *v
	}
	e.readTimers = readTimers
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		n := e.nodes[id]
		var p bool
		switch n.Kind {
		case NodeRail:
			p = true
		case NodeContact:
			p = power[e.ins[id][0]] && e.valueOf(n.Variable)
		case NodeNContact:
			p = power[e.ins[id][0]] && !e.valueOf(n.Variable)
		case NodeOR:
			for _, src := range e.ins[id] {
				p = p || power[src]
			}
		case NodeTON:
			en := power[e.ins[id][0]]
			prev := readTimers[n.Variable]
			if en {
				elapsed := prev.ElapsedMs + dtMs
				if elapsed > n.DelayMs {
					elapsed = n.DelayMs
				}
				pendingTimers[n.Variable] = TimerState{ElapsedMs: elapsed, Q: elapsed >= n.DelayMs}
			} else {
				// Releasing a non-retentive TON resets it; pausing the scanner
				// does not (no scans occur).
				pendingTimers[n.Variable] = TimerState{}
			}
			p = en
		case NodeCoil:
			en := power[e.ins[id][0]]
			pendingCoils[n.Variable] = en
			p = en
		}
		power[id] = p
		for _, dst := range e.outs[id] {
			indeg[dst]--
			if indeg[dst] == 0 {
				queue = append(queue, dst)
			}
		}
	}

	// Output phase: commit all coils together after logic evaluation.
	for _, n := range e.nodes {
		if n.Kind != NodeCoil {
			continue
		}
		v := pendingCoils[n.Variable]
		if strings.HasPrefix(n.Variable, "out.") {
			nextOutput[n.Variable] = v
		} else {
			nextIntern[n.Variable] = v
		}
	}
	e.intern = nextIntern
	e.outputs = nextOutput
	for k, v := range pendingTimers {
		*(e.timers[k]) = v
	}
	e.readTimers = nil
	e.scan++

	return e.snapshotLocked()
}

func (e *Engine) snapshotLocked() ScanReport {
	r := ScanReport{
		Scan:           e.scan,
		ProgramVersion: e.program.Version,
		Inputs:         map[string]bool{},
		Internals:      map[string]bool{},
		Outputs:        map[string]bool{},
		Timers:         map[string]TimerState{},
	}
	for k, v := range e.inputs {
		r.Inputs[k] = v
	}
	for k, v := range e.intern {
		r.Internals[k] = v
	}
	for k, v := range e.outputs {
		r.Outputs[k] = v
	}
	for k, v := range e.timers {
		r.Timers[k] = *v
	}
	return r
}

// Snapshot returns a deterministic, sorted copy for API/UI observers.
func (e *Engine) Snapshot() (ScanReport, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.scan == 0 {
		return ScanReport{
			ProgramVersion: e.program.Version,
			Inputs:         sortedBoolCopy(e.inputs),
			Internals:      sortedBoolCopy(e.intern),
			Outputs:        sortedBoolCopy(e.outputs),
			Timers:         timerCopy(e.timers),
		}, e.running
	}
	r := e.snapshotLocked()
	return r, e.running
}

func sortedBoolCopy(m map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func timerCopy(m map[string]*TimerState) map[string]TimerState {
	out := map[string]TimerState{}
	for k, v := range m {
		out[k] = *v
	}
	return out
}
