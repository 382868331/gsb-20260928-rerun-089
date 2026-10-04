package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newReviewTestServer(t *testing.T) (*Server, *Branch) {
	t.Helper()
	controller, err := NewController(demoProgram())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBranchManager(controller)
	controller.Start()
	controller.Scan()
	controller.Scan()
	boundary := manager.SaveBoundary("")
	controller.Scan()
	branch, err := manager.CreateBranch(boundary.ID, "审阅分支")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReplaceSegment(branch.ID, branch.BaseScan+1, []map[string]bool{{"start": true}}); err != nil {
		t.Fatal(err)
	}
	return &Server{controller: controller, branches: manager, storePath: ""}, branch
}

func TestReviewPackageAPIExportNoteAndReopen(t *testing.T) {
	server, branch := newReviewTestServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/branches/{id}/review-package", server.handleReviewExport)
	mux.HandleFunc("POST /api/reviews/import", server.handleReviewImport)
	mux.HandleFunc("POST /api/reviews/{id}/notes", server.handleReviewNote)

	exportReq := httptest.NewRequest(http.MethodPost, "/api/branches/"+branch.ID+"/review-package", nil)
	exportResponse := httptest.NewRecorder()
	mux.ServeHTTP(exportResponse, exportReq)
	if exportResponse.Code != http.StatusOK {
		t.Fatalf("导出失败: %d", exportResponse.Code)
	}
	var pkg ReviewPackage
	if err := json.Unmarshal(exportResponse.Body.Bytes(), &pkg); err != nil {
		t.Fatal(err)
	}
	if len(pkg.ComparedScans) != 1 || pkg.CurrentReport.MissingScans == nil {
		t.Fatalf("审阅包内容不完整: %+v", pkg)
	}

	noteBody, _ := json.Marshal(reviewNoteRequest{
		CompareVersion: pkg.CurrentReport.CompareVersion,
		Scan:           pkg.CurrentReport.Divergence.Scan,
		Text:           "审阅人确认首次差异",
	})
	noteReq := httptest.NewRequest(http.MethodPost, "/api/reviews/"+pkg.ID+"/notes", bytes.NewReader(noteBody))
	noteResponse := httptest.NewRecorder()
	mux.ServeHTTP(noteResponse, noteReq)
	if noteResponse.Code != http.StatusOK {
		t.Fatalf("说明保存失败: %d", noteResponse.Code)
	}

	data, _ := json.Marshal(pkg)
	importReq := httptest.NewRequest(http.MethodPost, "/api/reviews/import", bytes.NewReader(data))
	importResponse := httptest.NewRecorder()
	mux.ServeHTTP(importResponse, importReq)
	if importResponse.Code != http.StatusOK {
		t.Fatalf("重新打开失败: %d", importResponse.Code)
	}
	var imported ReviewPackage
	if err := json.Unmarshal(importResponse.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.ID == pkg.ID || imported.OriginalID != pkg.ID {
		t.Fatal("重新打开应保留原始 ID 并分配本地 ID")
	}
}

func TestReviewPackageRejectsUnknownReportVersion(t *testing.T) {
	server, branch := newReviewTestServer(t)
	pkg, err := server.branches.ExportReviewPackage(branch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.branches.AddReviewNote(pkg.ID, pkg.CurrentReport.CompareVersion+99, 0, "错误版本说明"); err == nil || !strings.Contains(err.Error(), "没有对照版本") {
		t.Fatalf("必须拒绝不存在的报告版本: %v", err)
	}
}
