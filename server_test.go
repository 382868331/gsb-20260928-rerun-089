package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
