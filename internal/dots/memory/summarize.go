package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Summarization runs on local Ollama. It costs no API spend, which is the whole
// reason the memory can afford to distill every session on a $20 plan.
//
// Ported from ~/.claude/hooks/save_to_obsidian.py, with one behavioural change:
// the input is a Clean, so the model only ever sees redacted text.
const (
	defaultOllamaHost = "http://localhost:11434"
	preferredModel    = "gemma4:12b"

	// maxTranscriptChars bounds what is sent to the model. The tail is kept
	// rather than the head: the end of a session is where the conclusions are,
	// and the head is mostly injected instructions.
	maxTranscriptChars = 24000

	// A session shorter than this had no content worth a note. Two questions
	// and an answer is not a thing to remember, and summarizing it costs the
	// same as summarizing a real one.
	minUserMessages = 2
	minConvoChars   = 400
)

const summaryPrompt = `You are summarizing a coding session for a personal knowledge base.

Write:
1. A one-line summary of what was accomplished (start with a verb).
2. 2-5 bullets covering decisions made, problems solved, and anything the user
   would need to remember weeks later.

Be specific: name files, commands, and error messages. Omit pleasantries and
tool mechanics. If the session reached no conclusion, say so plainly.

Do not invent detail that is not in the transcript. Some content may read as
"[redacted]" — that is deliberate, leave it as is.`

// Summarizer talks to Ollama.
type Summarizer struct {
	Host   string
	Model  string
	Client *http.Client
}

// NewSummarizer returns a Summarizer with the usual local defaults.
func NewSummarizer() *Summarizer {
	return &Summarizer{
		Host:  defaultOllamaHost,
		Model: preferredModel,
		// Generation on a 12B model is slow; the caller bounds the real wait
		// with a context, and this is only a backstop against a hung socket.
		Client: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Available reports whether Ollama is reachable. Capture uses this to skip
// distillation quietly rather than failing: no Ollama on a fleet box is a
// normal state, not an error.
func (s *Summarizer) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Host+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// resolveModel returns the configured model if installed, else the first
// non-embedding model on the box. Machines in the fleet have different models
// pulled, and a hardcoded name means the whole feature silently does nothing.
func (s *Summarizer) resolveModel(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Host+"/api/tags", nil)
	if err != nil {
		return "", err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "", err
	}

	var fallback string
	for _, m := range tags.Models {
		if m.Name == s.Model {
			return s.Model, nil
		}
		// Embedding models cannot chat, and picking one produces a confusing
		// error rather than an obvious "no usable model".
		if fallback == "" && !isEmbeddingModel(m.Name) {
			fallback = m.Name
		}
	}
	if fallback == "" {
		return "", fmt.Errorf("ollama has no chat-capable model installed")
	}
	return fallback, nil
}

func isEmbeddingModel(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "embed") || strings.Contains(n, "bge") || strings.Contains(n, "minilm")
}

// WorthSummarizing reports whether a session has enough substance to be worth a
// note and an Ollama call.
func WorthSummarizing(userMessages int, convo Clean) bool {
	return userMessages >= minUserMessages && len(strings.TrimSpace(string(convo))) >= minConvoChars
}

// Summarize distills a redacted transcript into note body text.
//
// It takes a Clean, not a string: the type is the guarantee that redaction
// already happened. The model is local, but "localhost so it does not matter"
// is how the ordering bug survived in the Python version — and a secret the
// model rewords on its way into the note is past every regex downstream.
func (s *Summarizer) Summarize(ctx context.Context, convo Clean) (string, error) {
	model, err := s.resolveModel(ctx)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(map[string]any{
		"model":  model,
		"stream": false,
		// Reasoning tokens would be paid for on every session and thrown away;
		// the note wants the answer, not the deliberation.
		"think": false,
		"options": map[string]any{
			"temperature": 0.3,
		},
		"messages": []map[string]string{
			{"role": "system", "content": summaryPrompt},
			{"role": "user", "content": tailTruncate(string(convo), maxTranscriptChars)},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama: %s", resp.Status)
	}

	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	text := strings.TrimSpace(out.Message.Content)
	if text == "" {
		return "", fmt.Errorf("ollama returned an empty summary")
	}
	return text, nil
}

// tailTruncate keeps the last n characters, cutting on a rune and then a line
// boundary so the model is not handed half a UTF-8 sequence or half a line.
func tailTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !isBoundary(s, cut) {
		cut++
	}
	s = s[cut:]
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < 500 {
		s = s[i+1:]
	}
	return "[earlier turns truncated]\n" + s
}
