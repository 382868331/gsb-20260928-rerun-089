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
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	BoundaryID   string                `json:"boundaryId"`
	ParentID     string                `json:"parentId,omitempty"`
	ParentName   string                `json:"parentName,omitempty"`
	ForkScan     int                   `json:"forkScan,omitempty"`
	BaseScan     int                   `json:"baseScan"`
	BaseVersion  int                   `json:"baseVersion"`
	InputVersion int                   `json:"inputVersion"`
	Status       string                `json:"status"`
	ProgramName  string                `json:"programName"`
	Program      Program               `json:"program"`
	BaseInputs   map[string]bool       `json:"baseInputs"`
	BaseValues   map[string]bool       `json:"baseValues"`
	BaseTimers   map[string]TimerState `json:"baseTimers"`
	Segment      []map[string]bool     `json:"segment"`
	Origins      []RowOrigin           `json:"origins"`
	Records      []ScanRecord          `json:"records"`
	Restarts     []ScanEvent           `json:"restarts"`
	OverlapNotes []string              `json:"overlapNotes"`
	Divergence   *Divergence           `json:"divergence,omitempty"`
	// Baseline 是子分支冻结的对照快照（父分支在派生或显式重建对照时
	// 的记录）：父分支之后替换输入不会悄悄改变已有子分支的比较依据。
	Baseline             []ScanRecord `json:"baseline"`
	BaselineInputVersion int          `json:"baselineInputVersion,omitempty"`
	CompareVersion       int          `json:"compareVersion"`
	Comparison           *Comparison  `json:"comparison,omitempty"`
	// Reports 保存被新对照版本替换下来的旧对照报告，只读保留。
	Reports []Comparison `json:"reports"`
}

// RowOrigin 说明分支中某一扫描实际采用的输入来自哪里：
// original=原执行真实扫描、parent=父分支模拟结果（非真实扫描）、
// self=本分支自己的替换。未执行的候选片段不会出现在这里。
type RowOrigin struct {
	Scan             int             `json:"scan"`
	Source           string          `json:"source"`
	SourceBranchID   string          `json:"sourceBranchId,omitempty"`
	SourceBranchName string          `json:"sourceBranchName,omitempty"`
	Inputs           map[string]bool `json:"inputs"`
	Note             string          `json:"note,omitempty"`
}

// Divergence 描述分支输出与原执行首次出现差异的位置及涉及的内部状态。
type Divergence struct {
	Scan          int                   `json:"scan"`
	CompareTarget string                `json:"compareTarget"`
	Outputs       []string              `json:"outputs"`
	Internals     []string              `json:"internals"`
	InternalState map[string]bool       `json:"internalState"`
	TimerState    map[string]TimerState `json:"timerState"`
	Message       string                `json:"message"`
}

// 比较状态：已比较且确实一致、已比较且存在差异、没有可比较的对照记录。
const (
	CompareStatusDiverged   = "diverged"
	CompareStatusConsistent = "consistent"
	CompareStatusMissing    = "missing"
)

// Comparison 是一次完整的对照结论：固定采用的程序修订、输入版本与
// 扫描范围，明确区分确实一致、存在真实差异和没有比较对象。
// 候选输入或缺失记录不会被当作已执行扫描而误判为一致。
type Comparison struct {
	CompareVersion     int         `json:"compareVersion"`
	Status             string      `json:"status"`
	CompareTarget      string      `json:"compareTarget"`
	ProgramVersion     int         `json:"programVersion"`
	InputVersion       int         `json:"inputVersion"`
	ParentInputVersion int         `json:"parentInputVersion,omitempty"`
	FromScan           int         `json:"fromScan"`
	ToScan             int         `json:"toScan"`
	ComparedScans      int         `json:"comparedScans"`
	MissingScans       []int       `json:"missingScans"`
	Divergence         *Divergence `json:"divergence,omitempty"`
	Message            string      `json:"message"`
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
