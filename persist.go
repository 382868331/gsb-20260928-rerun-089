package main

import (
	"encoding/json"
	"os"
)

// persistedController 是控制器（含扫描边界与试验分支）的持久化形态，
// 保存重开后各分支的输入、程序版本及扫描序号保持一致。
type persistedController struct {
	Mode           string                `json:"mode"`
	Version        int                   `json:"version"`
	Scans          int                   `json:"scans"`
	Program        Program               `json:"program"`
	Inputs         map[string]bool       `json:"inputs"`
	Values         map[string]bool       `json:"values"`
	Timers         map[string]TimerState `json:"timers"`
	History        []ScanRecord          `json:"history"`
	Log            []ScanEvent           `json:"log"`
	Boundaries     []*Boundary           `json:"boundaries"`
	Branches       []*Branch             `json:"branches"`
	NextBoundaryID int                   `json:"nextBoundaryId"`
	NextBranchID   int                   `json:"nextBranchId"`
}

// SaveToFile 把当前控制器状态原子写入磁盘。
func (c *Controller) SaveToFile(path string) error {
	snapshot := persistedController{
		Mode:           c.mode,
		Version:        c.version,
		Scans:          c.scans,
		Program:        c.program.program,
		Inputs:         cloneBools(c.inputs),
		Values:         cloneBools(c.values),
		Timers:         cloneTimers(c.timers),
		History:        c.history,
		Log:            c.log,
		Boundaries:     c.boundaries,
		Branches:       c.branches,
		NextBoundaryID: c.nextBoundaryID,
		NextBranchID:   c.nextBranchID,
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RestoreFromFile 从磁盘恢复控制器；文件不存在或损坏时返回错误，
// 调用方回退到全新控制器。恢复时运行态降级为暂停，避免重开即扫描。
func RestoreFromFile(path string) (*Controller, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snapshot persistedController
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	compiled, err := compileProgram(snapshot.Program)
	if err != nil {
		return nil, err
	}
	controller := &Controller{
		mode:           snapshot.Mode,
		version:        snapshot.Version,
		program:        compiled,
		inputs:         snapshot.Inputs,
		values:         snapshot.Values,
		timers:         snapshot.Timers,
		scans:          snapshot.Scans,
		log:            snapshot.Log,
		history:        snapshot.History,
		boundaries:     snapshot.Boundaries,
		branches:       snapshot.Branches,
		nextBoundaryID: snapshot.NextBoundaryID,
		nextBranchID:   snapshot.NextBranchID,
	}
	if controller.inputs == nil {
		controller.inputs = map[string]bool{}
	}
	if controller.values == nil {
		controller.values = map[string]bool{}
	}
	if controller.timers == nil {
		controller.timers = map[string]TimerState{}
	}
	for id := range compiled.inputs {
		if _, ok := controller.inputs[id]; !ok {
			controller.inputs[id] = false
		}
	}
	for _, node := range compiled.nodes {
		if node.Kind == "TON" {
			if _, ok := controller.timers[node.ID]; !ok {
				controller.timers[node.ID] = TimerState{}
			}
		}
	}
	for _, branch := range controller.branches {
		if branch.Inputs == nil {
			branch.Inputs = map[string]bool{}
		}
		if branch.Values == nil {
			branch.Values = map[string]bool{}
		}
		if branch.Timers == nil {
			branch.Timers = map[string]TimerState{}
		}
	}
	if controller.mode == "running" {
		controller.mode = "paused"
		controller.addLog("状态已从磁盘恢复，连续运行降级为暂停，需手动恢复")
	} else {
		controller.addLog("状态已从磁盘恢复")
	}
	return controller, nil
}
