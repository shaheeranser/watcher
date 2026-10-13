package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// maxResponseBytes bounds how much of a model response is read, so a
// misbehaving server cannot exhaust memory.
const maxResponseBytes = 1 << 20

// errSchemaUnsupported signals that the server rejected schema-constrained
// output, which is the cue to retry with plain JSON mode.
var errSchemaUnsupported = errors.New("ollama does not support schema-constrained output")

// OllamaOptions configures an Ollama backend. Timeout bounds one request
// (CORE-BE-8), MaxTokens bounds its generated length (CORE-BE-9), and
// MaxAttempts bounds retries of a failed request (a non-positive value means
// the default of three). KeepAlive is how long the server holds the model in
// memory between calls; zero leaves the server's own default.
type OllamaOptions struct {
	BaseURL     string
	Model       string
	Timeout     time.Duration
	MaxTokens   int
	KeepAlive   time.Duration
	MaxAttempts int
	Logger      *slog.Logger
}

// Ollama is a Backend backed by an Ollama server's chat API.
type Ollama struct {
	baseURL     string
	model       string
	timeout     time.Duration
	maxTokens   int
	maxAttempts int
	keepAlive   time.Duration
	logger      *slog.Logger
	backoff     time.Duration
	client      *http.Client
}

// NewOllama builds a client from opts. A nil Logger discards diagnostics; a
// non-positive MaxAttempts defaults to three.
func NewOllama(opts OllamaOptions) *Ollama {
	attempts := opts.MaxAttempts
	if attempts < 1 {
		attempts = 3
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Ollama{
		baseURL:     opts.BaseURL,
		model:       opts.Model,
		timeout:     opts.Timeout,
		maxTokens:   opts.MaxTokens,
		maxAttempts: attempts,
		keepAlive:   opts.KeepAlive,
		logger:      logger,
		backoff:     200 * time.Millisecond,
		client:      &http.Client{},
	}
}

func (o *Ollama) Name() string { return "ollama" }

// Model reports the model this backend was configured with, so a result can
// record which model produced the explanation rather than the backend kind.
func (o *Ollama) Model() string { return o.model }

func (o *Ollama) Explain(ctx context.Context, req Request) (Explanation, error) {
	prompt := buildPrompt(req)

	format := any(outputSchema())
	expl, err := o.call(ctx, prompt, format)
	if errors.Is(err, errSchemaUnsupported) {
		format = "json"
		expl, err = o.call(ctx, prompt, format)
	}
	for attempt := 1; err != nil; attempt++ {
		// A timeout means the model is too slow for the configured budget, not
		// that the request was unlucky: retrying reproduces the same too-slow
		// generation and only lengthens the wait before the incident is
		// reported. Cancellation is likewise terminal. CORE-BE-6's bounded
		// retry still applies to the transient errors below.
		if isTerminal(err) || attempt >= o.maxAttempts {
			return Explanation{}, fmt.Errorf("ollama explain failed after %d attempt(s): %w", attempt, err)
		}
		o.logger.Warn("ollama explain retrying", "model", o.model, "next_attempt", attempt+1, "error", err)
		if !sleepCtx(ctx, o.backoff*time.Duration(attempt)) {
			return Explanation{}, ctx.Err()
		}
		expl, err = o.call(ctx, prompt, format)
	}
	return ground(expl, req.Excerpt), nil
}

// isTerminal reports whether an error means retrying would only repeat it:
// either the per-request deadline elapsed or the run was cancelled.
func isTerminal(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func (o *Ollama) call(ctx context.Context, prompt string, format any) (Explanation, error) {
	payload, err := json.Marshal(chatRequest{
		Model:     o.model,
		Stream:    false,
		Format:    format,
		KeepAlive: keepAliveSeconds(o.keepAlive),
		Messages:  []chatMessage{{Role: "user", Content: prompt}},
		Options:   chatOptions{Temperature: 0, NumPredict: o.maxTokens},
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

	start := time.Now()
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
	expl, err := parseExplanation([]byte(chat.Message.Content))
	if err != nil {
		return Explanation{}, err
	}

	// The server's own timing separates queue time and model load from
	// generation, which is what makes a slow explanation diagnosable rather
	// than a mystery (PROD-NFR-4).
	o.logger.Info("ollama explained",
		"model", o.model,
		"elapsed", time.Since(start),
		"load_duration", time.Duration(chat.LoadDuration),
		"total_duration", time.Duration(chat.TotalDuration),
		"prompt_tokens", chat.PromptEvalCount,
		"eval_tokens", chat.EvalCount,
	)
	return expl, nil
}

// keepAliveSeconds encodes the keep-alive for Ollama's chat API, which accepts
// a number of seconds. Zero is omitted so the server applies its own default.
func keepAliveSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d.Seconds())
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
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	Stream    bool          `json:"stream"`
	Format    any           `json:"format,omitempty"`
	KeepAlive int           `json:"keep_alive,omitempty"`
	Options   chatOptions   `json:"options"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float64 `json:"temperature"`

	// NumPredict caps how many tokens the model may generate. Without it a
	// small model can loop and generate until the request times out, which
	// wastes the whole retry budget (CORE-BE-9).
	NumPredict int `json:"num_predict"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`

	// Durations are nanoseconds, as Ollama reports them.
	TotalDuration   int64 `json:"total_duration"`
	LoadDuration    int64 `json:"load_duration"`
	PromptEvalCount int   `json:"prompt_eval_count"`
	EvalCount       int   `json:"eval_count"`
}
