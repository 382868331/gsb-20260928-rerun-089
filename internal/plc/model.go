package plc

// NodeKind enumerates the node types supported by the virtual controller.
type NodeKind string

const (
	NodeRail     NodeKind = "rail"     // left power rail, single source of rung power
	NodeContact  NodeKind = "contact"  // normally-open contact reading a variable
	NodeNContact NodeKind = "ncontact" // normally-closed contact
	NodeOR       NodeKind = "or"       // merge point for parallel branches
	NodeCoil     NodeKind = "coil"     // energize an output / internal variable
	NodeTON      NodeKind = "ton"      // on-delay timer
)

// Node is one element in the ladder-style control graph.
type Node struct {
	ID       string   `json:"id"`
	Kind     NodeKind `json:"kind"`
	Variable string   `json:"variable,omitempty"`
	DelayMs  int64    `json:"delayMs,omitempty"`
	Label    string   `json:"label,omitempty"`
	X        float64  `json:"x,omitempty"`
	Y        float64  `json:"y,omitempty"`
}

// Edge is a directed wire carrying power from one node to another.
type Edge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// Program is an immutable, versioned control program.
type Program struct {
	Version int64  `json:"version"`
	Name    string `json:"name"`
	Nodes   []Node `json:"nodes"`
	Edges   []Edge `json:"edges"`
}

// TimerState persists across scans while a timer exists in the program.
type TimerState struct {
	ElapsedMs int64 `json:"elapsedMs"`
	Q         bool  `json:"q"`
}

// ScanReport is the observable outcome of one scan cycle.
type ScanReport struct {
	Scan           uint64                `json:"scan"`
	ProgramVersion int64                 `json:"programVersion"`
	Inputs         map[string]bool       `json:"inputs"`
	Internals      map[string]bool       `json:"internals"`
	Outputs        map[string]bool       `json:"outputs"`
	Timers         map[string]TimerState `json:"timers"`
}

// ValidateError describes why a program cannot be loaded.
type ValidateError struct {
	Message string `json:"message"`
	NodeID  string `json:"nodeId,omitempty"`
	EdgeID  string `json:"edgeId,omitempty"`
}

func (e ValidateError) Error() string {
	switch {
	case e.NodeID != "":
		return e.Message + " (node " + e.NodeID + ")"
	case e.EdgeID != "":
		return e.Message + " (wire " + e.EdgeID + ")"
	default:
		return e.Message
	}
}
