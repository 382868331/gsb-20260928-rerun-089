package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"virtual-plc/internal/plc"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	c := plc.NewController(50 * time.Millisecond)
	if errs := c.Load(plc.DemoProgram(1)); len(errs) != 0 {
		t.Fatalf("demo load: %v", errs)
	}
	return New(c)
}

func do(t *testing.T, h http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func statusOf(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	code, out := do(t, h, http.MethodGet, "/api/status", nil)
	if code != http.StatusOK {
		t.Fatalf("status code %d", code)
	}
	return out
}

func TestAPIInterlockFlow(t *testing.T) {
	h := newTestServer(t)

	// Press start, step twice: latch on and timer begins accumulating.
	do(t, h, http.MethodPost, "/api/inputs", map[string]any{"name": "in.start", "value": true})
	do(t, h, http.MethodPost, "/api/step", nil)
	do(t, h, http.MethodPost, "/api/inputs", map[string]any{"name": "in.start", "value": false})
	do(t, h, http.MethodPost, "/api/step", nil)

	// Run long enough for the 1500 ms delay: lamp comes on purely from
	// scheduled scans, never from a direct API write to the output.
	do(t, h, http.MethodPost, "/api/run", nil)
	time.Sleep(1800 * time.Millisecond)
	do(t, h, http.MethodPost, "/api/pause", nil)
	st := statusOf(t, h)
	report := st["report"].(map[string]any)
	outputs := report["outputs"].(map[string]any)
	if outputs["out.lamp"] != true {
		t.Fatalf("lamp should energize from real scans: %v", report)
	}
	if report["scan"].(float64) < 6 {
		t.Fatalf("expected multiple scans, got %v", report["scan"])
	}

	// Illegal program: cycle must be rejected with 422, active version unchanged.
	bad := plc.Program{
		Version: 2,
		Nodes: []plc.Node{
			{ID: "r", Kind: plc.NodeRail},
			{ID: "a", Kind: plc.NodeContact, Variable: "in.a"},
			{ID: "b", Kind: plc.NodeOR},
			{ID: "q", Kind: plc.NodeCoil, Variable: "out.q"},
		},
		Edges: []plc.Edge{
			{ID: "w1", Source: "r", Target: "a"},
			{ID: "w2", Source: "a", Target: "b"},
			{ID: "w3", Source: "b", Target: "a"},
			{ID: "w4", Source: "b", Target: "q"},
		},
	}
	code, out := do(t, h, http.MethodPost, "/api/program", bad)
	if code != http.StatusUnprocessableEntity || out["loaded"].(bool) {
		t.Fatalf("invalid program must be rejected: code=%d body=%v", code, out)
	}
	st = statusOf(t, h)
	if st["programVersion"].(float64) != 1 {
		t.Fatalf("active version must stay 1, got %v", st["programVersion"])
	}

	// SPA index served from embedded assets.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("<div id=\"root\">")) {
		t.Fatalf("SPA not served: code=%d", rec.Code)
	}
}
