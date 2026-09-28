package api

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"virtual-plc/internal/plc"
)

//go:embed all:assets
var assetsFS embed.FS

type Server struct {
	c   *plc.Controller
	mux *http.ServeMux
}

func New(c *plc.Controller) http.Handler {
	s := &Server{c: c, mux: http.NewServeMux()}
	s.routes()
	return logRequests(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/status", s.status)
	s.mux.HandleFunc("GET /api/program", s.getProgram)
	s.mux.HandleFunc("POST /api/program", s.loadProgram)
	s.mux.HandleFunc("POST /api/program/demo", s.loadDemo)
	s.mux.HandleFunc("POST /api/program/validate", s.validateOnly)
	s.mux.HandleFunc("POST /api/run", s.run)
	s.mux.HandleFunc("POST /api/pause", s.pause)
	s.mux.HandleFunc("POST /api/step", s.step)
	s.mux.HandleFunc("POST /api/inputs", s.setInput)

	static, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(static))
	s.mux.Handle("/", spaHandler(static, fileServer))
}

func spaHandler(root fs.FS, h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(root, p); err != nil {
			r.URL.Path = "/"
		}
		h.ServeHTTP(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	wait := r.URL.Query().Get("wait") == "1"
	if wait {
		// Long-poll by comparing serialized snapshots; no per-request
		// callback is retained, so abandoned requests cannot leak state.
		baseline, _ := json.Marshal(s.c.Status())
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			if cur, _ := json.Marshal(s.c.Status()); !bytes.Equal(cur, baseline) {
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, s.c.Status())
}

func (s *Server) getProgram(w http.ResponseWriter, r *http.Request) {
	p, ok := s.c.Program()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type loadResponse struct {
	Loaded  bool                `json:"loaded"`
	Errors  []plc.ValidateError `json:"errors,omitempty"`
	Program *plc.Program        `json:"program,omitempty"`
}

func decodeProgram(w http.ResponseWriter, r *http.Request) (*plc.Program, bool) {
	defer r.Body.Close()
	var p plc.Program
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, loadResponse{Errors: []plc.ValidateError{{Message: "invalid JSON: " + err.Error()}}})
		return nil, false
	}
	return &p, true
}

func (s *Server) validateOnly(w http.ResponseWriter, r *http.Request) {
	p, ok := decodeProgram(w, r)
	if !ok {
		return
	}
	errs, _ := p.Validate()
	writeJSON(w, http.StatusOK, loadResponse{Loaded: len(errs) == 0, Errors: errs, Program: p})
}

func (s *Server) loadProgram(w http.ResponseWriter, r *http.Request) {
	p, ok := decodeProgram(w, r)
	if !ok {
		return
	}
	errs := s.c.Load(p)
	if len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, loadResponse{Loaded: false, Errors: errs})
		return
	}
	writeJSON(w, http.StatusOK, loadResponse{Loaded: true, Program: p})
}

func (s *Server) loadDemo(w http.ResponseWriter, r *http.Request) {
	p, _ := s.c.Program()
	v := p.Version + 1
	// Demo always restarts at version 1 on an empty controller; otherwise bump.
	if v == 1 && p.Version == 0 {
		v = 1
	}
	demo := plc.DemoProgram(v)
	if errs := s.c.Load(demo); len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, loadResponse{Errors: errs})
		return
	}
	writeJSON(w, http.StatusOK, loadResponse{Loaded: true, Program: demo})
}

func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	s.c.Run()
	writeJSON(w, http.StatusOK, s.c.Status())
}
func (s *Server) pause(w http.ResponseWriter, r *http.Request) {
	s.c.Pause()
	writeJSON(w, http.StatusOK, s.c.Status())
}
func (s *Server) step(w http.ResponseWriter, r *http.Request) {
	s.c.SingleStep()
	writeJSON(w, http.StatusOK, s.c.Status())
}

type inputReq struct {
	Name  string `json:"name"`
	Value bool   `json:"value"`
}

func (s *Server) setInput(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req inputReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.c.SetInput(req.Name, req.Value) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown or non-input variable: " + req.Name})
		return
	}
	writeJSON(w, http.StatusOK, s.c.Status())
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		h.ServeHTTP(w, r)
	})
}
