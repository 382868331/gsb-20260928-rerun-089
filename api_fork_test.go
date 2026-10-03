package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 端到端验证：刚创建的分支所有切片必须是 []（不是 null），
// 前端读取 branch.segment.length 不会再白屏；三种状态可区分。
func TestAPINewBranchNeverReturnsNullSlices(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	boundary := manager.SaveBoundary("")
	branch, err := manager.CreateBranch(boundary.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(branch)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, field := range []string{"\"segment\":[]", "\"records\":[]", "\"origins\":[]", "\"restarts\":[]", "\"overlapNotes\":[]"} {
		if !strings.Contains(text, field) {
			t.Fatalf("新建分支 JSON 应包含 %s，实际：%s", field, text)
		}
	}
}

func TestFindTrialsIncludesAbandoned(t *testing.T) {
	controller, _ := NewController(demoProgram())
	manager := NewBranchManager(controller)
	controller.Start()
	controller.Scan()
	boundary := manager.SaveBoundary("")
	branch, _ := manager.CreateBranch(boundary.ID, "x")
	_, _ = manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{}})
	if _, err := manager.AbandonBranch(branch.ID); err != nil {
		t.Fatal(err)
	}
	_, branches := manager.FindTrials(controller.version, 1, 0)
	if len(branches) != 1 || branches[0].Status != branchStatusAbandoned {
		t.Fatal("已放弃分支仍应能按版本找回")
	}
}
