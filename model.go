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
	Boundaries  []BoundarySummary     `json:"boundaries"`
	Branches    []BranchView          `json:"branches"`
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

// ScanRecord 记录某次扫描后的输出与内部状态，
// 用于试验分支与原执行逐扫描比较。
type ScanRecord struct {
	Scan      int             `json:"scan"`
	Outputs   map[string]bool `json:"outputs"`
	Internals map[string]bool `json:"internals"`
}

// Boundary 是已保存的扫描边界：某一扫描序号上的完整状态快照，
// 作为试验分支的共同前缀起点，保存后只读。
type Boundary struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Scan    int                   `json:"scan"`
	Version int                   `json:"version"`
	Program Program               `json:"program"`
	Inputs  map[string]bool       `json:"inputs"`
	Values  map[string]bool       `json:"values"`
	Timers  map[string]TimerState `json:"timers"`
}

// InputOverride 在分支内替换某个后续扫描的虚拟输入采样值。
// Scan 为绝对扫描序号，必须大于分支起点（共同前缀只读）。
type InputOverride struct {
	Scan  int    `json:"scan"`
	Input string `json:"input"`
	Value bool   `json:"value"`
}

// Branch 是从某个扫描边界建立的输入试验分支。
// 分支持有独立的状态副本，不能修改原执行已经发生的输出。
type Branch struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	BoundaryID string                `json:"boundaryId"`
	OriginScan int                   `json:"originScan"`
	Version    int                   `json:"version"`
	Program    Program               `json:"program"`
	Inputs     map[string]bool       `json:"inputs"`
	Overrides  []InputOverride       `json:"overrides"`
	Scan       int                   `json:"scan"`
	Values     map[string]bool       `json:"values"`
	Timers     map[string]TimerState `json:"timers"`
	Records    []ScanRecord          `json:"records"`
	ResetLog   []ScanEvent           `json:"resetLog"`
}

// BranchView 是分支在状态接口中的展示形态，不含逐扫描记录。
type BranchView struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	BoundaryID string                `json:"boundaryId"`
	OriginScan int                   `json:"originScan"`
	Version    int                   `json:"version"`
	Scan       int                   `json:"scan"`
	Program    Program               `json:"program"`
	Inputs     map[string]bool       `json:"inputs"`
	Overrides  []InputOverride       `json:"overrides"`
	Outputs    map[string]bool       `json:"outputs"`
	Timers     map[string]TimerState `json:"timers"`
	ResetLog   []ScanEvent           `json:"resetLog"`
}

// BoundarySummary 是扫描边界在状态接口中的摘要。
type BoundarySummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Scan    int    `json:"scan"`
	Version int    `json:"version"`
}

// Divergence 描述分支与原执行比较后首次出现输出差异的位置
// 以及该扫描上涉及的内部状态。
type Divergence struct {
	Found         bool     `json:"found"`
	Scan          int      `json:"scan"`
	Outputs       []string `json:"outputs"`
	Internals     []string `json:"internals"`
	ComparedScans int      `json:"comparedScans"`
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
