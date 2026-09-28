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
}

func main() {
	controller, err := NewController(demoProgram())
	if err != nil {
		log.Fatal(err)
	}
	server := &Server{controller: controller}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", server.handleState)
	mux.HandleFunc("POST /api/start", server.handleStart)
	mux.HandleFunc("POST /api/pause", server.handlePause)
	mux.HandleFunc("POST /api/stop", server.handleStop)
	mux.HandleFunc("POST /api/scans", server.handleScan)
	mux.HandleFunc("PUT /api/inputs/{id}", server.handleInput)
	mux.HandleFunc("PUT /api/program", server.handleProgram)
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
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
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
	compiled, err := compileProgram(program)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	_ = compiled
	s.stopTicker()
	s.mu.Lock()
	if err := s.controller.Load(program); err != nil {
		s.mu.Unlock()
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	state := s.controller.State()
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, state)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
