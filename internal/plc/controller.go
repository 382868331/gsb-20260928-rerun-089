package plc

import (
	"sync"
	"time"
)

// Controller owns the active program version and its scan scheduler.
// Load replaces the program as a new version atomically. The scan ticker is
// stopped/started without discarding engine state: pause/resume only gates
// scheduling, so TON accumulators and latched values survive.
type Controller struct {
	mu       sync.Mutex
	engine   *Engine
	tick     time.Duration
	running  bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
	last     ScanReport
	hasScan  bool
	onChange []func()
}

func NewController(tick time.Duration) *Controller {
	if tick <= 0 {
		tick = 100 * time.Millisecond
	}
	return &Controller{tick: tick}
}

func (c *Controller) Load(p *Program) []ValidateError {
	eng, errs := Load(p)
	if len(errs) > 0 {
		return errs
	}
	c.mu.Lock()
	wasRunning := c.running
	c.stopSchedulerLocked()
	c.engine = eng
	c.running = false
	c.hasScan = false
	c.mu.Unlock()
	if wasRunning {
		c.Run()
	}
	c.notify()
	return nil
}

func (c *Controller) HasProgram() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.engine != nil
}

// Run starts scheduling scans. It does not reset any state.
func (c *Controller) Run() bool {
	c.mu.Lock()
	if c.engine == nil || c.running {
		ok := c.engine != nil && c.running
		c.mu.Unlock()
		return ok
	}
	c.running = true
	c.stopCh = make(chan struct{})
	stop := c.stopCh
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		t := time.NewTicker(c.tick)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				c.doScan()
			}
		}
	}()
	c.mu.Unlock()
	c.notify()
	return true
}

// Pause halts scheduling. Timer elapsed time is held untouched.
func (c *Controller) Pause() {
	c.mu.Lock()
	c.stopSchedulerLocked()
	c.mu.Unlock()
	c.notify()
}

func (c *Controller) stopSchedulerLocked() {
	if c.running {
		close(c.stopCh)
		c.running = false
		c.wg.Wait()
	}
}

func (c *Controller) doScan() {
	c.mu.Lock()
	eng := c.engine
	c.mu.Unlock()
	if eng == nil {
		return
	}
	r := eng.Scan(c.tick.Milliseconds())
	c.mu.Lock()
	c.last = r
	c.hasScan = true
	c.mu.Unlock()
	c.notify()
}

// SingleStep runs exactly one scan and pauses scheduling. Used to inspect
// that observed outputs really come from a scan cycle.
func (c *Controller) SingleStep() (ScanReport, bool) {
	c.mu.Lock()
	if c.engine == nil {
		c.mu.Unlock()
		return ScanReport{}, false
	}
	c.stopSchedulerLocked()
	eng := c.engine
	c.mu.Unlock()
	r := eng.Scan(c.tick.Milliseconds())
	c.mu.Lock()
	c.last = r
	c.hasScan = true
	c.mu.Unlock()
	c.notify()
	return r, true
}

func (c *Controller) SetInput(name string, value bool) bool {
	c.mu.Lock()
	eng := c.engine
	c.mu.Unlock()
	if eng == nil {
		return false
	}
	ok := eng.SetInput(name, value)
	if ok {
		c.notify()
	}
	return ok
}

type Status struct {
	Running        bool       `json:"running"`
	TickMs         int64      `json:"tickMs"`
	ProgramVersion int64      `json:"programVersion"`
	ProgramName    string     `json:"programName"`
	Report         ScanReport `json:"report"`
}

func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{Running: c.running, TickMs: c.tick.Milliseconds()}
	if c.engine != nil {
		p := *c.engine.program
		s.ProgramVersion = p.Version
		s.ProgramName = p.Name
		if c.hasScan {
			s.Report = c.last
		} else {
			r, _ := c.engine.Snapshot()
			s.Report = r
		}
	}
	return s
}

func (c *Controller) Program() (Program, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.engine == nil {
		return Program{}, false
	}
	return *c.engine.program, true
}

func (c *Controller) OnChange(f func()) {
	c.mu.Lock()
	c.onChange = append(c.onChange, f)
	c.mu.Unlock()
}

func (c *Controller) notify() {
	c.mu.Lock()
	fs := append([]func(){}, c.onChange...)
	c.mu.Unlock()
	for _, f := range fs {
		f()
	}
}
