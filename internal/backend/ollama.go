package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxResponseBytes bounds how much of a model response is read, so a
// misbehaving server cannot exhaust memory.
const maxResponseBytes = 1 << 20

// errSchemaUnsupported signals that the server rejected schema-constrained
// output, which is the cue to retry with plain JSON mode.
var errSchemaUnsupported = errors.New("ollama does not support schema-constrained output")

// Ollama is a Backend backed by an Ollama server's chat API.
type Ollama struct {
	baseURL     string
	model       string
	timeout     time.Duration
	maxAttempts int
	backoff     time.Duration
	client      *http.Client
}

// NewOllama builds a client for the given server and model. timeout bounds each
// individual request (CORE-BE-8); maxAttempts defaults to three.
func NewOllama(baseURL, model string, timeout time.Duration) *Ollama {
	return &Ollama{
		baseURL:     baseURL,
		model:       model,
		timeout:     timeout,
		maxAttempts: 3,
		backoff:     200 * time.Millisecond,
		client:      &http.Client{},
	}
}

func (o *Ollama) Name() string { return "ollama" }

func (o *Ollama) Explain(ctx context.Context, req Request) (Explanation, error) {
	prompt := buildPrompt(req)

	format := any(outputSchema())
	expl, err := o.call(ctx, prompt, format)
	if errors.Is(err, errSchemaUnsupported) {
		format = "json"
		expl, err = o.call(ctx, prompt, format)
	}
	if err != nil {
		for attempt := 1; attempt < o.maxAttempts; attempt++ {
			if !sleepCtx(ctx, o.backoff*time.Duration(attempt)) {
				return Explanation{}, ctx.Err()
			}
			if expl, err = o.call(ctx, prompt, format); err == nil {
				break
			}
		}
	}
	if err != nil {
		return Explanation{}, fmt.Errorf("ollama explain failed after %d attempts: %w", o.maxAttempts, err)
	}
	return ground(expl, req.Excerpt), nil
}

func (o *Ollama) call(ctx context.Context, prompt string, format any) (Explanation, error) {
	payload, err := json.Marshal(chatRequest{
		Model:    o.model,
		Stream:   false,
		Format:   format,
		Messages: []chatMessage{{Role: "user", Content: prompt}},
		Options:  chatOptions{Temperature: 0},
	})
	if err != nil {
		return Explanation{}, fmt.Errorf("encode request: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return Explanation{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return Explanation{}, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Explanation{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusBadRequest {
		if _, isSchema := format.(map[string]any); isSchema {
			return Explanation{}, errSchemaUnsupported
		}
	}
	if resp.StatusCode != http.StatusOK {
		return Explanation{}, fmt.Errorf("ollama status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var chat chatResponse
	if err := json.Unmarshal(body, &chat); err != nil {
		return Explanation{}, fmt.Errorf("decode chat response: %w", err)
	}
	return parseExplanation([]byte(chat.Message.Content))
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Format   any           `json:"format,omitempty"`
	Options  chatOptions   `json:"options"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float64 `json:"temperature"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`
}
