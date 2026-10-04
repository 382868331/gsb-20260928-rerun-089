package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

type Server struct {
	mu         sync.Mutex
	controller *Controller
	branches   *BranchManager
	storePath  string
	ticker     *time.Ticker
	done       chan struct{}
	wg         sync.WaitGroup
}

func main() {
	controller, err := NewController(demoProgram())
	if err != nil {
		log.Fatal(err)
	}
	manager := NewBranchManager(controller)
	server := &Server{controller: controller, branches: manager, storePath: "branch_store.json"}
	if err := manager.Load(server.storePath); err != nil {
		log.Printf("分支存储载入失败，按空存储继续：%v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", server.handleState)
	mux.HandleFunc("POST /api/start", server.handleStart)
	mux.HandleFunc("POST /api/pause", server.handlePause)
	mux.HandleFunc("POST /api/stop", server.handleStop)
	mux.HandleFunc("POST /api/scans", server.handleScan)
	mux.HandleFunc("PUT /api/inputs/{id}", server.handleInput)
	mux.HandleFunc("PUT /api/program", server.handleProgram)
	mux.HandleFunc("POST /api/program/preview", server.handleProgramPreview)
	mux.HandleFunc("POST /api/boundaries", server.handleBoundary)
	mux.HandleFunc("POST /api/branches", server.handleBranchCreate)
	mux.HandleFunc("POST /api/branches/{id}/fork", server.handleBranchFork)
	mux.HandleFunc("POST /api/branches/{id}/abandon", server.handleBranchAbandon)
	mux.HandleFunc("POST /api/branches/{id}/rebase", server.handleBranchRebase)
	mux.HandleFunc("GET /api/trials", server.handleTrials)
	mux.HandleFunc("GET /api/branches/{id}/review-package", server.handleReviewPackage)
	mux.HandleFunc("POST /api/branches/{id}/review-notes", server.handleReviewNote)
	mux.HandleFunc("PUT /api/branches/{id}/segment", server.handleBranchSegment)
	mux.HandleFunc("POST /api/branches/{id}/restart", server.handleBranchRestart)
	staticFiles, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServerFS(staticFiles))
	log.Println("虚拟控制器监听 http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func (s *Server) startTicker() {
	s.stopTicker()
	s.ticker = time.NewTicker(time.Duration(scanIntervalMs) * time.Millisecond)
	s.done = make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.done:
				return
			case <-s.ticker.C:
				s.mu.Lock()
				s.controller.Scan()
				s.mu.Unlock()
			}
		}
	}()
}

func (s *Server) stopTicker() {
	if s.ticker != nil {
		s.ticker.Stop()
		s.ticker = nil
	}
	if s.done != nil {
		close(s.done)
		s.wg.Wait()
		s.done = nil
	}
}

func (s *Server) handleState(writer http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	state := s.controller.State()
	s.attachBranches(&state)
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

// attachBranches 把扫描边界与试验分支摘要附加到状态响应。
// 调用方必须已持有锁。
func (s *Server) attachBranches(state *ControllerState) {
	if s.branches == nil {
		return
	}
	state.Boundaries = s.branches.Boundaries()
	state.Branches = s.branches.Branches()
}

// persist 把边界与分支保存到磁盘；保存重开后各分支的输入段、
// 程序版本与扫描序号保持一致。
func (s *Server) persist() {
	if s.branches == nil || s.storePath == "" {
		return
	}
	if err := s.branches.Save(s.storePath); err != nil {
		log.Printf("分支存储保存失败：%v", err)
	}
}

func (s *Server) handleStart(writer http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.controller.Start()
	state := s.controller.State()
	s.mu.Unlock()
	s.startTicker()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handlePause(writer http.ResponseWriter, _ *http.Request) {
	s.stopTicker()
	s.mu.Lock()
	s.controller.Pause()
	state := s.controller.State()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handleStop(writer http.ResponseWriter, _ *http.Request) {
	s.stopTicker()
	s.mu.Lock()
	s.controller.Stop()
	state := s.controller.State()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

type scanRequest struct {
	Count int `json:"count"`
}

func (s *Server) handleScan(writer http.ResponseWriter, request *http.Request) {
	req := scanRequest{Count: 1}
	if request.ContentLength > 0 {
		if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if req.Count < 1 || req.Count > 100 {
		http.Error(writer, "count 必须在 1 到 100 之间", http.StatusBadRequest)
		return
	}
	s.stopTicker()
	s.mu.Lock()
	s.controller.Start()
	var state ControllerState
	for range req.Count {
		state = s.controller.Scan()
	}
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

type inputRequest struct {
	Value bool `json:"value"`
}

func (s *Server) handleInput(writer http.ResponseWriter, request *http.Request) {
	var req inputRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if err := s.controller.SetInput(request.PathValue("id"), req.Value); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusNotFound)
		return
	}
	state := s.controller.State()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handleProgram(writer http.ResponseWriter, request *http.Request) {
	var program Program
	if err := json.NewDecoder(request.Body).Decode(&program); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := compileProgram(program); err != nil {
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	s.stopTicker()
	s.mu.Lock()
	var err error
	if request.URL.Query().Get("mode") == "preserve" {
		err = s.controller.LoadPreserve(program)
	} else {
		err = s.controller.Load(program)
	}
	if err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.mu.Unlock()
	if state.Mode == "running" {
		s.startTicker()
	}
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handleProgramPreview(writer http.ResponseWriter, request *http.Request) {
	var program Program
	if err := json.NewDecoder(request.Body).Decode(&program); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	diff, err := s.controller.Preview(program)
	s.mu.Unlock()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	writeJSON(writer, http.StatusOK, diff)
}

type boundaryRequest struct {
	Label string `json:"label"`
}

func (s *Server) handleBoundary(writer http.ResponseWriter, request *http.Request) {
	req := boundaryRequest{}
	if request.ContentLength > 0 {
		if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.mu.Lock()
	s.branches.SaveBoundary(req.Label)
	state := s.controller.State()
	s.attachBranches(&state)
	s.persist()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

type branchCreateRequest struct {
	BoundaryID string `json:"boundaryId"`
	Name       string `json:"name"`
}

func (s *Server) handleBranchCreate(writer http.ResponseWriter, request *http.Request) {
	var req branchCreateRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if _, err := s.branches.CreateBranch(req.BoundaryID, req.Name); err != nil {
		s.mu.Unlock()
		// 创建失败不改变任何运行状态，已有边界、分支与暂停程序全部保留。
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.attachBranches(&state)
	s.persist()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

type branchForkRequest struct {
	Name     string `json:"name"`
	ForkScan int    `json:"forkScan"`
}

func (s *Server) handleBranchFork(writer http.ResponseWriter, request *http.Request) {
	var req branchForkRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ForkScan == 0 {
		http.Error(writer, "forkScan 必须指定父分支上已模拟的扫描序号", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if _, err := s.branches.ForkBranch(request.PathValue("id"), req.Name, req.ForkScan); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.attachBranches(&state)
	s.persist()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handleBranchRebase(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	if _, err := s.branches.RebaseComparison(request.PathValue("id")); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.attachBranches(&state)
	s.persist()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handleBranchAbandon(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	if _, err := s.branches.AbandonBranch(request.PathValue("id")); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.attachBranches(&state)
	s.persist()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

// handleTrials 支持按程序修订版本、输入版本与扫描序号找回历史试验。
func (s *Server) handleTrials(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	programVersion := atoiOrZero(query.Get("programVersion"))
	inputVersion := atoiOrZero(query.Get("inputVersion"))
	scan := atoiOrZero(query.Get("scan"))
	s.mu.Lock()
	boundaries, branches := s.branches.FindTrials(programVersion, inputVersion, scan)
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{"boundaries": boundaries, "branches": branches})
}

func atoiOrZero(value string) int {
	if value == "" {
		return 0
	}
	result := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0
		}
		result = result*10 + int(char-'0')
	}
	return result
}

func (s *Server) handleReviewPackage(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	pkg, err := s.branches.ExportReviewPackage(request.PathValue("id"))
	s.mu.Unlock()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	writer.Header().Set("Content-Disposition", `attachment; filename="branch-review-package.json"`)
	writeJSON(writer, http.StatusOK, pkg)
}

type reviewNoteRequest struct {
	CompareVersion int    `json:"compareVersion"`
	Text           string `json:"text"`
}

func (s *Server) handleReviewNote(writer http.ResponseWriter, request *http.Request) {
	var req reviewNoteRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if req.CompareVersion < 1 {
		http.Error(writer, "compareVersion 必须大于 0", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if _, err := s.branches.AddReviewNote(request.PathValue("id"), req.CompareVersion, req.Text); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.attachBranches(&state)
	s.persist()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

type branchSegmentRequest struct {
	FromScan int               `json:"fromScan"`
	Inputs   []map[string]bool `json:"inputs"`
}

func (s *Server) handleBranchSegment(writer http.ResponseWriter, request *http.Request) {
	var req branchSegmentRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if _, err := s.branches.ReplaceSegment(request.PathValue("id"), req.FromScan, req.Inputs); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.attachBranches(&state)
	s.persist()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

type branchRestartRequest struct {
	NodeID string `json:"nodeId"`
}

func (s *Server) handleBranchRestart(writer http.ResponseWriter, request *http.Request) {
	var req branchRestartRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if _, err := s.branches.RestartElement(request.PathValue("id"), req.NodeID); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.attachBranches(&state)
	s.persist()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
