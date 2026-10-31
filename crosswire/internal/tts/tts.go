// Package tts turns payload text into speech audio through an
// OpenAI-compatible /audio/speech endpoint. There is no pure-Go TTS worth
// shipping, and bundling an OS speech tool breaks the single-binary goal, so
// the network is the honest option. A user may always upload a WAV instead.
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Speak renders text as 16-bit PCM WAV. voice defaults to "alloy".
func Speak(ctx context.Context, text, voice string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty speech text")
	}
	key := first("CROSSWIRE_LLM_API_KEY", "OPENAI_API_KEY")
	if key == "" {
		return nil, errors.New("speech needs CROSSWIRE_LLM_API_KEY or OPENAI_API_KEY, or upload a WAV")
	}
	base := first("CROSSWIRE_LLM_BASE_URL", "OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := os.Getenv("CROSSWIRE_TTS_MODEL")
	if model == "" {
		model = "gpt-4o-mini-tts"
	}
	if voice == "" {
		voice = "alloy"
	}
	body, _ := json.Marshal(map[string]any{
		"model":           model,
		"input":           text,
		"voice":           voice,
		"response_format": "wav",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		if len(raw) > 300 {
			raw = raw[:300]
		}
		return nil, fmt.Errorf("tts returned %d: %s", resp.StatusCode, raw)
	}
	if !bytes.HasPrefix(raw, []byte("RIFF")) {
		return nil, errors.New("tts did not return WAV audio")
	}
	return raw, nil
}

func first(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}
