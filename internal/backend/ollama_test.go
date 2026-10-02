package backend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const validJSON = `{"summary":"nil dereference","likely_cause":"handler used a nil pointer","evidence":["panic: boom"],"suggested_fix":"guard the pointer","confidence":0.8,"severity":"high"}`

func chatBody(content string) string {
	b, _ := json.Marshal(map[string]any{
		"message": map[string]string{"role": "assistant", "content": content},
		"done":    true,
	})
	return string(b)
}

type received struct {
	Model   string `json:"model"`
	Stream  bool   `json:"stream"`
	Format  any    `json:"format"`
	Options struct {
		Temperature float64 `json:"temperature"`
		NumPredict  int     `json:"num_predict"`
	} `json:"options"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func TestExplainParsesValidResponse(t *testing.T) {
	var got received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		io.WriteString(w, chatBody(validJSON))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "test-model", time.Second, 512)
	req := Request{Kind: "go-panic", Source: "app.log", Excerpt: "panic: boom\n\t/app/main.go:1 +0x0"}
	expl, err := o.Explain(context.Background(), req)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if expl.Severity != "high" || expl.Confidence != 0.8 {
		t.Errorf("unexpected explanation: %+v", expl)
	}
	if expl.LikelyCause != "handler used a nil pointer" {
		t.Errorf("LikelyCause = %q", expl.LikelyCause)
	}

	if got.Model != "test-model" || got.Stream || got.Options.Temperature != 0 {
		t.Errorf("request shape wrong: %+v", got)
	}
	if got.Options.NumPredict != 512 {
		t.Errorf("num_predict = %d, want the configured cap 512", got.Options.NumPredict)
	}
	if o.Model() != "test-model" || o.Name() != "ollama" {
		t.Errorf("Model/Name = %q/%q, want test-model/ollama", o.Model(), o.Name())
	}
	if _, ok := got.Format.(map[string]any); !ok {
		t.Errorf("expected a JSON schema for format, got %T", got.Format)
	}
	if len(got.Messages) != 1 || !strings.Contains(got.Messages[0].Content, "Detector kind: go-panic") {
		t.Errorf("prompt missing expected content: %+v", got.Messages)
	}
}

func TestExplainRetriesMalformedThenSucceeds(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n < 3 {
			io.WriteString(w, chatBody("not json at all"))
			return
		}
		io.WriteString(w, chatBody(validJSON))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m", time.Second, 512)
	o.backoff = time.Millisecond
	if _, err := o.Explain(context.Background(), Request{Excerpt: "panic: boom"}); err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestExplainGivesUpAfterAttempts(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		io.WriteString(w, chatBody("still not json"))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m", time.Second, 512)
	o.backoff = time.Millisecond
	_, err := o.Explain(context.Background(), Request{Excerpt: "x"})
	if err == nil {
		t.Fatal("expected an error after exhausting attempts")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestExplainRetriesOnServerError(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m", time.Second, 512)
	o.backoff = time.Millisecond
	if _, err := o.Explain(context.Background(), Request{Excerpt: "x"}); err == nil {
		t.Fatal("expected an error on repeated 500s")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestExplainTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		io.WriteString(w, chatBody(validJSON))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m", 30*time.Millisecond, 512)
	o.backoff = time.Millisecond
	start := time.Now()
	if _, err := o.Explain(context.Background(), Request{Excerpt: "x"}); err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("timeout handling took too long: %s", elapsed)
	}
}

func TestExplainFallsBackToJSONFormat(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req received
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		calls++
		mu.Unlock()

		if _, isSchema := req.Format.(map[string]any); isSchema {
			http.Error(w, `{"error":"format is not supported"}`, http.StatusBadRequest)
			return
		}
		io.WriteString(w, chatBody(validJSON))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m", time.Second, 512)
	if _, err := o.Explain(context.Background(), Request{Excerpt: "panic: boom"}); err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (schema then json)", calls)
	}
}

func TestGroundingDropsUngroundedEvidence(t *testing.T) {
	content := `{"summary":"cause","likely_cause":"x","evidence":["panic: boom","an invented line"],"suggested_fix":"fix","confidence":0.9,"severity":"medium"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, chatBody(content))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m", time.Second, 512)
	expl, err := o.Explain(context.Background(), Request{Excerpt: "panic: boom\n\t/app/main.go:1 +0x0"})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if len(expl.Evidence) != 1 || expl.Evidence[0] != "panic: boom" {
		t.Errorf("evidence = %v, want only the grounded entry", expl.Evidence)
	}
	if expl.Confidence > 0.3 {
		t.Errorf("confidence = %v, want floored to <= 0.3", expl.Confidence)
	}
	if !strings.Contains(expl.Summary, "confidence lowered") {
		t.Errorf("summary should note the drop: %q", expl.Summary)
	}
}

func TestParseExplanationValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", validJSON, false},
		{"fenced", "```json\n" + validJSON + "\n```", false},
		{"no object", "I cannot help with that", true},
		{"bad severity", `{"summary":"s","likely_cause":"c","evidence":[],"suggested_fix":"f","confidence":0.5,"severity":"very-high"}`, true},
		{"confidence range", `{"summary":"s","likely_cause":"c","evidence":[],"suggested_fix":"f","confidence":1.5,"severity":"low"}`, true},
		{"empty summary", `{"summary":"  ","likely_cause":"c","evidence":[],"suggested_fix":"f","confidence":0.5,"severity":"low"}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseExplanation([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
