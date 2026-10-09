package onboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProberPingAndHasModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"models":[{"name":"qwen2.5:1.5b"},{"name":"llama3:latest"}]}`)
	}))
	defer srv.Close()

	p := NewProber(srv.URL)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	for _, name := range []string{"qwen2.5:1.5b", "llama3"} {
		ok, err := p.HasModel(context.Background(), name)
		if err != nil {
			t.Fatalf("HasModel(%q): %v", name, err)
		}
		if !ok {
			t.Errorf("HasModel(%q) = false, want true", name)
		}
	}
	ok, err := p.HasModel(context.Background(), "missing")
	if err != nil || ok {
		t.Errorf("HasModel(missing) = %v, %v; want false, nil", ok, err)
	}
}

func TestProberPull(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/pull" {
			http.NotFound(w, r)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"status":"success"}`)
	}))
	defer srv.Close()

	if err := NewProber(srv.URL).Pull(context.Background(), "qwen2.5:1.5b"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got["name"] != "qwen2.5:1.5b" {
		t.Errorf("pull body = %v, want the model name", got)
	}
}

func TestProberUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	if err := NewProber(url).Ping(context.Background()); err == nil {
		t.Error("Ping to a closed server should error")
	}
}
