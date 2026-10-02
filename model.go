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
	Boundaries  []Boundary            `json:"boundaries,omitempty"`
	Branches    []Branch              `json:"branches,omitempty"`
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

// ScanRecord 记录一次已执行扫描的输入、输出与内部状态，
// 作为试验分支比较的共同前缀（只读，分支不能修改）。
type ScanRecord struct {
	Scan      int                   `json:"scan"`
	Version   int                   `json:"version"`
	Inputs    map[string]bool       `json:"inputs"`
	Outputs   map[string]bool       `json:"outputs"`
	Internals map[string]bool       `json:"internals"`
	Timers    map[string]TimerState `json:"timers"`
}

// Boundary 是用户保存的扫描边界：某一扫描序号上的完整状态快照，
// 试验分支从这里分叉。边界保存当时的程序版本，修订载入后旧边界仍可分叉。
type Boundary struct {
	ID      string                `json:"id"`
	Label   string                `json:"label"`
	Scan    int                   `json:"scan"`
	Version int                   `json:"version"`
	Program Program               `json:"program"`
	Inputs  map[string]bool       `json:"inputs"`
	Values  map[string]bool       `json:"values"`
	Timers  map[string]TimerState `json:"timers"`
}

// Branch 是从某个扫描边界建立的输入试验分支：用一段替换输入
// 从边界快照重新模拟，与原执行历史比较首次输出差异。
type Branch struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	BoundaryID  string                `json:"boundaryId"`
	BaseScan    int                   `json:"baseScan"`
	BaseVersion int                   `json:"baseVersion"`
	ProgramName string                `json:"programName"`
	Program     Program               `json:"program"`
	BaseInputs  map[string]bool       `json:"baseInputs"`
	BaseValues  map[string]bool       `json:"baseValues"`
	BaseTimers  map[string]TimerState `json:"baseTimers"`
	Segment     []map[string]bool     `json:"segment"`
	Records     []ScanRecord          `json:"records"`
	Restarts    []ScanEvent           `json:"restarts"`
	Divergence  *Divergence           `json:"divergence,omitempty"`
}

// Divergence 描述分支输出与原执行首次出现差异的位置及涉及的内部状态。
type Divergence struct {
	Scan      int      `json:"scan"`
	Outputs   []string `json:"outputs"`
	Internals []string `json:"internals"`
	Message   string   `json:"message"`
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
