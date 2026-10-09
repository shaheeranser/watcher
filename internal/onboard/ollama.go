package onboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Prober checks a live Ollama server. Onboarding uses it to verify the URL the
// operator typed before writing anything (INST-ONB-4, INST-ONB-12), to see
// whether the chosen model is present, and to pull it when it is not
// (INST-ONB-5).
type Prober interface {
	Ping(ctx context.Context) error
	HasModel(ctx context.Context, name string) (bool, error)
	Pull(ctx context.Context, name string) error
}

// probeTimeout bounds the small metadata calls; a model pull is bounded by the
// caller's context instead, since downloads run long.
const probeTimeout = 10 * time.Second

type ollamaProber struct {
	baseURL string
	client  *http.Client
}

// NewProber builds a Prober for the given base URL.
func NewProber(baseURL string) Prober {
	return &ollamaProber{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

func (p *ollamaProber) Ping(ctx context.Context) error {
	_, err := p.tags(ctx)
	return err
}

func (p *ollamaProber) HasModel(ctx context.Context, name string) (bool, error) {
	models, err := p.tags(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range models {
		// Ollama reports an untagged name as ":latest", so accept both.
		if m == name || m == name+":latest" {
			return true, nil
		}
	}
	return false, nil
}

func (p *ollamaProber) Pull(ctx context.Context, name string) error {
	body, err := json.Marshal(map[string]any{"name": name, "stream": false})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("pull %s: %w", name, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull %s: ollama status %d", name, resp.StatusCode)
	}
	return nil
}

// tags lists the locally available models. It doubles as the connectivity
// check: a server that answers it is reachable.
func (p *ollamaProber) tags(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama status %d", resp.StatusCode)
	}

	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode /api/tags: %w", err)
	}
	names := make([]string, 0, len(payload.Models))
	for _, m := range payload.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
