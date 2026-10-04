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
	// BaseInputVersion 固定对照方（父分支）在派生/重建对照时的输入版本；
	// 根分支对照原执行实时历史，该值为 0。
	BaseInputVersion int `json:"baseInputVersion"`
	// BaselineRecords 是派生时对父分支已模拟记录的快照，作为子分支的
	// 对照依据；父分支之后替换输入不会悄悄改变它。
	BaselineRecords []ScanRecord `json:"baselineRecords"`
	// Reports 是历次对照报告（含旧报告），最新一份在末尾。
	Reports []CompareReport `json:"reports"`
	// CompareStale 为 true 表示父分支输入版本已更新，当前对照依据已过期，
	// 需显式建立新的对照版本（旧报告保留）。
	CompareStale bool `json:"compareStale"`
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

// 比较结果取值：已比较且发现首处差异 / 已比较且确实一致 / 没有比较对象。
const (
	CompareResultDiverged   = "diverged"
	CompareResultIdentical  = "identical"
	CompareResultNoBaseline = "no-baseline"
)

// CompareReport 是一次固定依据的对照报告：比较固定采用程序修订、
// 对照方输入版本与对应扫描范围。缺失记录的扫描列为 missing，
// 不当作已执行扫描，也不能据此判定一致。历次报告全部保留。
type CompareReport struct {
	ProgramVersion     int         `json:"programVersion"`
	BaseInputVersion   int         `json:"baseInputVersion"`
	BranchInputVersion int         `json:"branchInputVersion"`
	ScanFrom           int         `json:"scanFrom"`
	ScanTo             int         `json:"scanTo"`
	Compared           int         `json:"compared"`
	Missing            []int       `json:"missing"`
	Result             string      `json:"result"`
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
