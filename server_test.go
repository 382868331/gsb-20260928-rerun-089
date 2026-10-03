package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newBranchTestServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		controller: controller,
		branches:   NewBranchManager(controller),
		storePath:  filepath.Join(t.TempDir(), "store.json"),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/scans", server.handleScan)
	mux.HandleFunc("PUT /api/inputs/{id}", server.handleInput)
	mux.HandleFunc("POST /api/boundaries", server.handleBoundary)
	mux.HandleFunc("POST /api/branches", server.handleBranchCreate)
	mux.HandleFunc("PUT /api/branches/{id}/segment", server.handleBranchSegment)
	mux.HandleFunc("POST /api/branches/{id}/restart", server.handleBranchRestart)
	mux.HandleFunc("POST /api/branches/{id}/fork", server.handleBranchFork)
	mux.HandleFunc("POST /api/branches/{id}/abandon", server.handleBranchAbandon)
	mux.HandleFunc("POST /api/branches/{id}/rebase", server.handleBranchRebase)
	return server, mux
}

func callAPI(t *testing.T, mux *http.ServeMux, method, path string, body any) (int, ControllerState) {
	t.Helper()
	payload, _ := json.Marshal(body)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(method, path, bytes.NewReader(payload)))
	var state ControllerState
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
	}
	return response.Code, state
}

func TestBranchAPIEndToEnd(t *testing.T) {
	server, mux := newBranchTestServer(t)

	callAPI(t, mux, http.MethodPut, "/api/inputs/start", inputRequest{Value: true})
	code, state := callAPI(t, mux, http.MethodPost, "/api/scans", scanRequest{Count: 3})
	if code != http.StatusOK || state.ScanNumber != 3 {
		t.Fatalf("扫描失败: %d", code)
	}
	code, state = callAPI(t, mux, http.MethodPost, "/api/boundaries", boundaryRequest{Label: "互锁边界"})
	if code != http.StatusOK || len(state.Boundaries) != 1 {
		t.Fatalf("保存边界失败: %d", code)
	}
	boundary := state.Boundaries[0]
	if boundary.Scan != 3 || boundary.Version != state.Version {
		t.Fatalf("边界应记录当前扫描序号与版本: %+v", boundary)
	}
	// 原执行继续推进，供分支比较共同前缀之后的差异。
	callAPI(t, mux, http.MethodPost, "/api/scans", scanRequest{Count: 2})

	code, state = callAPI(t, mux, http.MethodPost, "/api/branches", branchCreateRequest{BoundaryID: boundary.ID})
	if code != http.StatusOK || len(state.Branches) != 1 {
		t.Fatalf("建立分支失败: %d", code)
	}
	branch := state.Branches[0]

	// 共同前缀只读。
	code, _ = callAPI(t, mux, http.MethodPut, "/api/branches/"+branch.ID+"/segment",
		branchSegmentRequest{FromScan: branch.BaseScan, Inputs: []map[string]bool{{"stop": true}}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("共同前缀替换应被拒绝，得到 %d", code)
	}

	code, state = callAPI(t, mux, http.MethodPut, "/api/branches/"+branch.ID+"/segment",
		branchSegmentRequest{FromScan: branch.BaseScan + 1, Inputs: []map[string]bool{{"stop": true}}})
	if code != http.StatusOK {
		t.Fatalf("替换输入段失败: %d", code)
	}
	if state.Branches[0].Divergence == nil {
		t.Fatal("停止脉冲应产生首次输出差异")
	}

	code, state = callAPI(t, mux, http.MethodPost, "/api/branches/"+branch.ID+"/restart",
		branchRestartRequest{NodeID: "runDelay"})
	if code != http.StatusOK || len(state.Branches[0].Restarts) != 1 {
		t.Fatalf("重新开始元件应留下记录: %d", code)
	}

	// 保存重开后分支的输入、程序版本及扫描序号保持一致。
	reloaded := NewBranchManager(server.controller)
	if err := reloaded.Load(server.storePath); err != nil {
		t.Fatal(err)
	}
	restored := reloaded.Branches()
	if len(restored) != 1 || restored[0].BaseScan != branch.BaseScan || restored[0].BaseVersion != branch.BaseVersion {
		t.Fatal("重开后分支扫描序号与程序版本必须一致")
	}
	if len(restored[0].Segment) != 1 || !restored[0].Segment[0]["stop"] {
		t.Fatal("重开后分支替换输入段必须一致")
	}
}

// 页面级语义：未来扫描不是已比较一致；中间层可放弃且后代继续工作；
// 父分支替换后子分支对照冻结，显式重建才更新。
func TestBranchAPIComparisonSemantics(t *testing.T) {
	_, mux := newBranchTestServer(t)

	callAPI(t, mux, http.MethodPut, "/api/inputs/start", inputRequest{Value: true})
	callAPI(t, mux, http.MethodPost, "/api/scans", scanRequest{Count: 1})
	callAPI(t, mux, http.MethodPut, "/api/inputs/start", inputRequest{Value: false})
	callAPI(t, mux, http.MethodPost, "/api/scans", scanRequest{Count: 3})
	_, state := callAPI(t, mux, http.MethodPost, "/api/boundaries", boundaryRequest{Label: "b"})
	boundary := state.Boundaries[0]

	_, state = callAPI(t, mux, http.MethodPost, "/api/branches", branchCreateRequest{BoundaryID: boundary.ID, Name: "父"})
	parent := state.Branches[0]
	// 分支模拟了原执行没有的未来扫描：必须是没有比较对象，而不是一致。
	_, state = callAPI(t, mux, http.MethodPut, "/api/branches/"+parent.ID+"/segment",
		branchSegmentRequest{FromScan: parent.BaseScan + 1, Inputs: []map[string]bool{{}, {}, {}}})
	comparison := state.Branches[0].Comparison
	if comparison == nil || comparison.Status != CompareStatusMissing {
		t.Fatalf("未来扫描必须是没有比较对象: %+v", comparison)
	}

	// 原执行补两个扫描后，分支变为部分可比较。
	callAPI(t, mux, http.MethodPost, "/api/scans", scanRequest{Count: 2})
	_, state = callAPI(t, mux, http.MethodPut, "/api/branches/"+parent.ID+"/segment",
		branchSegmentRequest{FromScan: parent.BaseScan + 1, Inputs: []map[string]bool{{}, {}, {}}})
	comparison = state.Branches[0].Comparison
	if comparison == nil || comparison.Status != CompareStatusConsistent || len(comparison.MissingScans) != 1 {
		t.Fatalf("部分可比较时应为确实一致并标出缺失扫描: %+v", comparison)
	}

	// 派生子分支后父分支替换输入：子分支对照冻结，显式重建才更新。
	_, state = callAPI(t, mux, http.MethodPost, "/api/branches/"+parent.ID+"/fork",
		branchForkRequest{Name: "子", ForkScan: parent.BaseScan + 2})
	child := state.Branches[1]
	_, state = callAPI(t, mux, http.MethodPut, "/api/branches/"+parent.ID+"/segment",
		branchSegmentRequest{FromScan: parent.BaseScan + 2, Inputs: []map[string]bool{{"stop": true}, {}}})
	for _, branch := range state.Branches {
		if branch.ID == child.ID {
			child = branch
		}
	}
	if child.Comparison == nil || child.Comparison.ParentInputVersion != 2 {
		t.Fatalf("父分支替换后子分支对照必须冻结在派生时的输入版本: %+v", child.Comparison)
	}
	code, state := callAPI(t, mux, http.MethodPost, "/api/branches/"+child.ID+"/rebase", nil)
	if code != http.StatusOK {
		t.Fatalf("显式重建对照失败: %d", code)
	}
	for _, branch := range state.Branches {
		if branch.ID == child.ID {
			child = branch
		}
	}
	if child.CompareVersion != 2 || len(child.Reports) != 1 || child.Comparison.ParentInputVersion != 3 {
		t.Fatalf("重建后应固定新输入版本并保留旧报告: %+v", child.Comparison)
	}

	// 中间层可放弃：转为只读保留，子分支记录仍在。
	code, state = callAPI(t, mux, http.MethodPost, "/api/branches/"+parent.ID+"/abandon", nil)
	if code != http.StatusOK {
		t.Fatalf("有子分支的中间层应可放弃: %d", code)
	}
	for _, branch := range state.Branches {
		if branch.ID == parent.ID && branch.Status != branchStatusAbandoned {
			t.Fatal("放弃后中间层必须为只读保留状态")
		}
	}
	code, _ = callAPI(t, mux, http.MethodPut, "/api/branches/"+parent.ID+"/segment",
		branchSegmentRequest{FromScan: parent.BaseScan + 1, Inputs: []map[string]bool{{}}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("放弃后的中间层不能再编辑，得到 %d", code)
	}
}

func TestScanAPIProducesRealOutputsAndRejectsInvalidProgram(t *testing.T) {
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{controller: controller}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/inputs/{id}", server.handleInput)
	mux.HandleFunc("POST /api/scans", server.handleScan)
	mux.HandleFunc("PUT /api/program", server.handleProgram)

	body, _ := json.Marshal(inputRequest{Value: true})
	request := httptest.NewRequest(http.MethodPut, "/api/inputs/start", bytes.NewReader(body))
	request.SetPathValue("id", "start")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("设置输入失败: %s", response.Body.String())
	}

	body, _ = json.Marshal(scanRequest{Count: 1})
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/scans", bytes.NewReader(body)))
	var state ControllerState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !state.Outputs["motor"] {
		t.Fatal("扫描 API 应返回真实扫描后的电机保持输出")
	}

	illegal := demoProgram()
	illegal.Edges = illegal.Edges[:1]
	body, _ = json.Marshal(illegal)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/program", bytes.NewReader(body)))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("非法程序应被拒绝，得到 %d", response.Code)
	}
}
