package main

type Program struct {
	Name    string `json:"name"`
	Nodes   []Node `json:"nodes"`
	Edges   []Edge `json:"edges"`
	Version string `json:"version,omitempty"`
}

type Node struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Label    string         `json:"label"`
	Position map[string]int `json:"position,omitempty"`
	PresetMs int            `json:"presetMs,omitempty"`
}

type Edge struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	SourcePort string `json:"sourcePort"`
	TargetPort string `json:"targetPort"`
}

type ControllerState struct {
	Mode        string                `json:"mode"`
	Version     int                   `json:"version"`
	ProgramName string                `json:"programName"`
	Program     Program               `json:"program"`
	ScanNumber  int                   `json:"scanNumber"`
	ScanMs      int                   `json:"scanMs"`
	Inputs      map[string]bool       `json:"inputs"`
	Internals   map[string]any        `json:"internals"`
	Outputs     map[string]bool       `json:"outputs"`
	Timers      map[string]TimerState `json:"timers"`
	Log         []ScanEvent           `json:"log"`
}

// ProgramDiff 描述待载入程序相对当前程序的修订影响，
// 用于在暂停状态下预览后再决定保留兼容状态或明确重新开始。
type ProgramDiff struct {
	Compatible    bool     `json:"compatible"`
	Paused        bool     `json:"paused"`
	TimerChanges  []string `json:"timerChanges"`
	RenamedInputs []string `json:"renamedInputs"`
	RemovedNodes  []string `json:"removedNodes"`
	AddedNodes    []string `json:"addedNodes"`
	Messages      []string `json:"messages"`
}

type TimerState struct {
	ElapsedMs int  `json:"elapsedMs"`
	Q         bool `json:"q"`
}

type ScanEvent struct {
	Scan    int    `json:"scan"`
	Message string `json:"message"`
}

type CompiledProgram struct {
	program Program
	nodes   map[string]Node
	order   []string
	inputs  map[string]bool
	sources map[string]map[string]PortRef
}

type PortRef struct {
	Node string `json:"node"`
	Port string `json:"port"`
}
