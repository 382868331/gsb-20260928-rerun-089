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
	mux.HandleFunc("POST /api/branches/{id}/fork", server.handleBranchFork)
	mux.HandleFunc("POST /api/branches/{id}/abandon", server.handleBranchAbandon)
	mux.HandleFunc("POST /api/branches/{id}/rebaseline", server.handleBranchRebaseline)
	mux.HandleFunc("GET /api/trials", server.handleTrials)
	mux.HandleFunc("PUT /api/branches/{id}/segment", server.handleBranchSegment)
	mux.HandleFunc("POST /api/branches/{id}/restart", server.handleBranchRestart)
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

// 通过 API 完成三层分支、重叠替换、放弃中间层、显式重建对照与保存重开；
// 并验证缺失对照区间与非法程序载入都不会改写原执行。
func TestBranchAPIThreeLevelsAbandonRebaselineAndReload(t *testing.T) {
	server, mux := newBranchTestServer(t)

	callAPI(t, mux, http.MethodPut, "/api/inputs/start", inputRequest{Value: true})
	callAPI(t, mux, http.MethodPost, "/api/scans", scanRequest{Count: 1})
	callAPI(t, mux, http.MethodPut, "/api/inputs/start", inputRequest{Value: false})
	_, state := callAPI(t, mux, http.MethodPost, "/api/scans", scanRequest{Count: 5})
	scansBefore := state.ScanNumber
	_, state = callAPI(t, mux, http.MethodPost, "/api/boundaries", boundaryRequest{Label: "三层边界"})
	boundary := state.Boundaries[0]
	callAPI(t, mux, http.MethodPost, "/api/scans", scanRequest{Count: 2})

	// 一层：替换 3 段输入。
	_, state = callAPI(t, mux, http.MethodPost, "/api/branches", branchCreateRequest{BoundaryID: boundary.ID, Name: "一层"})
	level1 := state.Branches[0]
	_, state = callAPI(t, mux, http.MethodPut, "/api/branches/"+level1.ID+"/segment",
		branchSegmentRequest{FromScan: level1.BaseScan + 1, Inputs: []map[string]bool{{}, {}, {}}})
	level1 = state.Branches[0]

	// 二层：派生后重叠替换（覆盖一层的一段候选输入）。
	_, state = callAPI(t, mux, http.MethodPost, "/api/branches/"+level1.ID+"/fork",
		branchForkRequest{Name: "二层", ForkScan: level1.BaseScan + 1})
	level2 := state.Branches[1]
	_, state = callAPI(t, mux, http.MethodPut, "/api/branches/"+level2.ID+"/segment",
		branchSegmentRequest{FromScan: level2.BaseScan + 1, Inputs: []map[string]bool{{}, {}}})
	level2 = state.Branches[1]
	if len(level2.OverlapNotes) == 0 {
		t.Fatal("重叠替换必须注明实际采用的输入及来源")
	}

	// 三层：派生并替换停止脉冲，应对照二层快照报告首处真实差异。
	_, state = callAPI(t, mux, http.MethodPost, "/api/branches/"+level2.ID+"/fork",
		branchForkRequest{Name: "三层", ForkScan: level2.BaseScan + 1})
	level3 := state.Branches[2]
	_, state = callAPI(t, mux, http.MethodPut, "/api/branches/"+level3.ID+"/segment",
		branchSegmentRequest{FromScan: level3.BaseScan + 1, Inputs: []map[string]bool{{"stop": true}}})
	level3 = state.Branches[2]
	if len(level3.Reports) == 0 || level3.Reports[len(level3.Reports)-1].Result != CompareResultDiverged {
		t.Fatal("三层应对照二层快照报告首处真实输出差异")
	}

	// 放弃中间层：转为只读保留记录，三层继续工作。
	code, state := callAPI(t, mux, http.MethodPost, "/api/branches/"+level2.ID+"/abandon", nil)
	if code != http.StatusOK {
		t.Fatalf("有后代的中间层应可放弃: %d", code)
	}
	code, _ = callAPI(t, mux, http.MethodPut, "/api/branches/"+level2.ID+"/segment",
		branchSegmentRequest{FromScan: level2.BaseScan + 1, Inputs: []map[string]bool{{}}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("放弃的中间层只读，替换应被拒绝，得到 %d", code)
	}
	code, state = callAPI(t, mux, http.MethodPut, "/api/branches/"+level3.ID+"/segment",
		branchSegmentRequest{FromScan: level3.BaseScan + 1, Inputs: []map[string]bool{{}}})
	if code != http.StatusOK {
		t.Fatalf("中间层放弃后三层应可继续替换: %d", code)
	}

	// 一层后续替换输入：三层的对照依据不能悄悄变化。
	reportsBefore := 0
	for _, branch := range state.Branches {
		if branch.ID == level3.ID {
			reportsBefore = len(branch.Reports)
			if branch.CompareStale {
				t.Fatal("一层尚未替换，三层对照依据不应过期")
			}
		}
	}
	// 新派生一个一层子分支，验证父分支替换后的过期与显式重建语义。
	_, state = callAPI(t, mux, http.MethodPost, "/api/branches/"+level1.ID+"/fork",
		branchForkRequest{Name: "一层新子", ForkScan: level1.BaseScan + 1})
	freshID := ""
	for i := range state.Branches {
		if state.Branches[i].Name == "一层新子" {
			freshID = state.Branches[i].ID
			if state.Branches[i].CompareStale {
				t.Fatal("新派生的子分支应对照最新输入版本，不应过期")
			}
		}
	}
	if freshID == "" {
		t.Fatal("派生一层新子失败")
	}
	_, state = callAPI(t, mux, http.MethodPut, "/api/branches/"+level1.ID+"/segment",
		branchSegmentRequest{FromScan: level1.BaseScan + 2, Inputs: []map[string]bool{{"stop": true}, {}}})
	for i := range state.Branches {
		if state.Branches[i].ID == freshID && !state.Branches[i].CompareStale {
			t.Fatal("父分支替换输入后，子分支对照依据应标记为已过期")
		}
		if state.Branches[i].ID == level3.ID && len(state.Branches[i].Reports) != reportsBefore {
			t.Fatal("一层替换不能悄悄改变三层已有的对照报告")
		}
	}
	code, state = callAPI(t, mux, http.MethodPost, "/api/branches/"+freshID+"/rebaseline", nil)
	if code != http.StatusOK {
		t.Fatalf("显式建立新的对照版本失败: %d", code)
	}
	for i := range state.Branches {
		if state.Branches[i].ID == freshID {
			if state.Branches[i].CompareStale {
				t.Fatal("重建对照版本后不应再标记过期")
			}
			if len(state.Branches[i].Reports) < 2 {
				t.Fatal("重建后应追加新报告且保留旧报告")
			}
		}
	}

	// 原执行未被任何分支操作改写。
	_, finalState := callAPI(t, mux, http.MethodPost, "/api/scans", scanRequest{Count: 1})
	if finalState.ScanNumber != scansBefore+3 {
		t.Fatalf("原执行扫描计数应只随真实扫描推进，得到 #%d", finalState.ScanNumber)
	}

	// 保存重开：边界、分支、报告与放弃状态一致；只读历史可按修订与扫描序号找回。
	reloaded := NewBranchManager(server.controller)
	if err := reloaded.Load(server.storePath); err != nil {
		t.Fatal(err)
	}
	restored := reloaded.Branches()
	if len(restored) != len(state.Branches) {
		t.Fatalf("重开后分支数量应一致，得到 %d", len(restored))
	}
	for _, branch := range restored {
		if branch.ID == level2.ID && branch.Status != branchStatusAbandoned {
			t.Fatal("重开后中间层应仍为已放弃")
		}
		if branch.ID == level3.ID && len(branch.Reports) == 0 {
			t.Fatal("重开后三层的对照报告必须保留")
		}
	}
	_, found := reloaded.FindTrials(level1.BaseVersion, 0, level3.BaseScan+1)
	if len(found) == 0 {
		t.Fatal("按程序修订与扫描序号应能找回只读历史（含已放弃层）")
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
