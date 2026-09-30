package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestProgramSavedReloadedAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "program.json")
	program := customInterlockProgram()
	if err := saveProgram(path, program); err != nil {
		t.Fatal(err)
	}
	restored, err := loadStoredProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Nodes) != len(program.Nodes) || restored.Name != program.Name {
		t.Fatal("重启后必须恢复已保存程序的全部节点和名称")
	}
	compiled, err := compileProgram(restored)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := compiled.inputs["iOn"]; !exists {
		t.Fatal("恢复程序的输入列表必须来自保存内容")
	}
	if _, exists := compiled.inputs["start"]; exists {
		t.Fatal("恢复程序不能混入内置示例输入")
	}
}
func TestGetProgramAndInvalidLoadKeepsRunningVersion(t *testing.T) {
	controller, err := NewController(customInterlockProgram())
	if err != nil {
		t.Fatal(err)
	}
	controller.Start()
	controller.Pause()
	server := &Server{controller: controller, programPath: filepath.Join(t.TempDir(), "program.json")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/program", server.handleGetProgram)
	mux.HandleFunc("PUT /api/program", server.handleProgram)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/program", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("读取当前程序失败: %s", response.Body.String())
	}
	var current Program
	if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if len(current.Nodes) != 10 {
		t.Fatal("GET /api/program 必须返回当前发布版本的全部元件")
	}

	illegal := customInterlockProgram()
	illegal.Edges = nil
	body, _ := json.Marshal(illegal)
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/program", bytes.NewReader(body))
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("非法连接程序必须返回 422，得到 %d: %s", response.Code, response.Body.String())
	}
	if controller.mode != "paused" || controller.version != 1 {
		t.Fatal("非法程序被拒绝后旧版本和暂停状态必须原样保留")
	}
}
