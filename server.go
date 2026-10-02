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
	ticker     *time.Ticker
	done       chan struct{}
	wg         sync.WaitGroup
	statePath  string
}

func main() {
	controller, err := NewController(demoProgram())
	if err != nil {
		log.Fatal(err)
	}
	statePath := "controller_state.json"
	if restored, rerr := RestoreFromFile(statePath); rerr == nil {
		controller = restored
	}
	server := &Server{controller: controller, statePath: statePath}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", server.handleState)
	mux.HandleFunc("POST /api/start", server.handleStart)
	mux.HandleFunc("POST /api/pause", server.handlePause)
	mux.HandleFunc("POST /api/stop", server.handleStop)
	mux.HandleFunc("POST /api/scans", server.handleScan)
	mux.HandleFunc("PUT /api/inputs/{id}", server.handleInput)
	mux.HandleFunc("PUT /api/program", server.handleProgram)
	mux.HandleFunc("POST /api/program/preview", server.handleProgramPreview)
	mux.HandleFunc("POST /api/boundaries", server.handleBoundarySave)
	mux.HandleFunc("POST /api/branches", server.handleBranchCreate)
	mux.HandleFunc("PUT /api/branches/{id}/overrides", server.handleBranchOverrides)
	mux.HandleFunc("POST /api/branches/{id}/scans", server.handleBranchScan)
	mux.HandleFunc("GET /api/branches/{id}/compare", server.handleBranchCompare)
	mux.HandleFunc("POST /api/branches/{id}/reset", server.handleBranchReset)
	staticFiles, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServerFS(staticFiles))
	log.Println("虚拟控制器监听 http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// persist 在状态变更后落盘，保存重开后各分支的输入、程序版本及
// 扫描序号保持一致；落盘失败不阻断操作，仅记录日志。
func (s *Server) persist() {
	if s.statePath == "" {
		return
	}
	s.mu.Lock()
	err := s.controller.SaveToFile(s.statePath)
	s.mu.Unlock()
	if err != nil {
		log.Printf("状态保存失败: %v", err)
	}
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
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handleStart(writer http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.controller.Start()
	state := s.controller.State()
	s.mu.Unlock()
	s.startTicker()
	s.persist()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handlePause(writer http.ResponseWriter, _ *http.Request) {
	s.stopTicker()
	s.mu.Lock()
	s.controller.Pause()
	state := s.controller.State()
	s.mu.Unlock()
	s.persist()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handleStop(writer http.ResponseWriter, _ *http.Request) {
	s.stopTicker()
	s.mu.Lock()
	s.controller.Stop()
	state := s.controller.State()
	s.mu.Unlock()
	s.persist()
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
	s.persist()
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
	s.persist()
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
		stillRunning := s.controller.mode == "running"
		s.mu.Unlock()
		if stillRunning {
			s.startTicker()
		}
		http.Error(writer, "载入失败，原程序与运行状态保持不变："+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.mu.Unlock()
	if state.Mode == "running" {
		s.startTicker()
	}
	s.persist()
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

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

type boundaryRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleBoundarySave(writer http.ResponseWriter, request *http.Request) {
	req := boundaryRequest{}
	if request.ContentLength > 0 {
		if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.mu.Lock()
	s.controller.SaveBoundary(req.Name)
	state := s.controller.State()
	s.mu.Unlock()
	s.persist()
	writeJSON(writer, http.StatusOK, state)
}

type branchRequest struct {
	BoundaryID string `json:"boundaryId"`
	Name       string `json:"name"`
}

func (s *Server) handleBranchCreate(writer http.ResponseWriter, request *http.Request) {
	var req branchRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if _, err := s.controller.CreateBranch(req.BoundaryID, req.Name); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusNotFound)
		return
	}
	state := s.controller.State()
	s.mu.Unlock()
	s.persist()
	writeJSON(writer, http.StatusOK, state)
}

type overridesRequest struct {
	Overrides []InputOverride `json:"overrides"`
}

func (s *Server) handleBranchOverrides(writer http.ResponseWriter, request *http.Request) {
	var req overridesRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if err := s.controller.SetBranchOverrides(request.PathValue("id"), req.Overrides); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.mu.Unlock()
	s.persist()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handleBranchScan(writer http.ResponseWriter, request *http.Request) {
	req := scanRequest{Count: 1}
	if request.ContentLength > 0 {
		if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.mu.Lock()
	if _, err := s.controller.ScanBranch(request.PathValue("id"), req.Count); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.mu.Unlock()
	s.persist()
	writeJSON(writer, http.StatusOK, state)
}

func (s *Server) handleBranchCompare(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	divergence, err := s.controller.CompareBranch(request.PathValue("id"))
	s.mu.Unlock()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(writer, http.StatusOK, divergence)
}

type resetElementRequest struct {
	Node string `json:"node"`
}

func (s *Server) handleBranchReset(writer http.ResponseWriter, request *http.Request) {
	var req resetElementRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if err := s.controller.ResetBranchElement(request.PathValue("id"), req.Node); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.mu.Unlock()
	s.persist()
	writeJSON(writer, http.StatusOK, state)
}
