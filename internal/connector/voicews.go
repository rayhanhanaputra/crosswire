package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/guard"
	"github.com/rayhanhanaputra/crosswire/internal/model"
	"github.com/rayhanhanaputra/crosswire/internal/wsock"
)

const (
	maxVoiceAudioBytes = 6 << 20
	maxVoiceEventBytes = 64 << 10

	defaultVoiceChunk  = `{"type":"audio.chunk","audio":"{{audio_chunk_base64}}"}`
	defaultVoiceEnd    = `{"type":"audio.end"}`
	defaultVoiceCancel = `{"type":"session.cancel"}`
)

// VoiceDefaults are the templates and done marker a voice-ws target gets when
// it sets none.
func VoiceDefaults() model.VoiceWSConfig {
	return model.VoiceWSConfig{ChunkBytes: 16384, MaxEvents: 100, ChunkTemplate: defaultVoiceChunk,
		EndTemplate: defaultVoiceEnd, CancelTemplate: defaultVoiceCancel, DonePath: "type", DoneValue: "session.done"}
}

// sendVoiceWS streams the audio artifact over a WebSocket in bounded chunks.
// It sends the start event (the target's request template), one chunk event
// per ChunkBytes of audio, then the end event, and collects reply events until
// the done marker appears, the event limit is reached or the timeout passes.
// The reply's transcript paths, actions and audio are gathered across events.
func sendVoiceWS(ctx context.Context, t model.Target, endpoint string, start map[string]any, in Input) (model.Exchange, error) {
	ex := model.Exchange{Request: start}
	began := time.Now()
	if _, err := guard.ParseURL(endpoint, "ws", "wss"); err != nil {
		return fail(ex, began, fmt.Errorf("voice-ws target: %w", err))
	}
	if in.AudioBase64 == "" {
		return fail(ex, began, errors.New("voice-ws needs an audio artifact (speech, dtmf, audio-upload)"))
	}
	audio, err := base64.StdEncoding.DecodeString(in.AudioBase64)
	if err != nil {
		return fail(ex, began, errors.New("audio artifact is not valid base64"))
	}
	if len(audio) > maxVoiceAudioBytes {
		return fail(ex, began, errors.New("voice audio exceeds 6 MiB"))
	}
	cfg := t.VoiceWS
	def := VoiceDefaults()
	if cfg.ChunkBytes <= 0 {
		cfg.ChunkBytes = def.ChunkBytes
	}
	if cfg.MaxEvents <= 0 {
		cfg.MaxEvents = def.MaxEvents
	}
	cfg.ChunkTemplate = orDefault(cfg.ChunkTemplate, def.ChunkTemplate)
	cfg.EndTemplate = orDefault(cfg.EndTemplate, def.EndTemplate)
	cfg.CancelTemplate = orDefault(cfg.CancelTemplate, def.CancelTemplate)
	cfg.DonePath = orDefault(cfg.DonePath, def.DonePath)
	cfg.DoneValue = orDefault(cfg.DoneValue, def.DoneValue)

	// The chunk event never carries the whole recording, only its slice.
	chunkIn := in
	chunkIn.AudioBase64 = ""
	end, err := render(cfg.EndTemplate, defaultVoiceEnd, chunkIn)
	if err != nil {
		return fail(ex, began, fmt.Errorf("end template: %w", err))
	}
	hdr, err := wsHeaders(t)
	if err != nil {
		return fail(ex, began, err)
	}
	chunks := (len(audio) + cfg.ChunkBytes - 1) / cfg.ChunkBytes
	ex.Wire = map[string]any{"transport": "voice-ws", "audio_bytes": len(audio), "chunk_bytes": cfg.ChunkBytes, "chunks": chunks, "max_events": cfg.MaxEvents}

	ctx, cancel := context.WithTimeout(ctx, Timeout(t))
	defer cancel()
	conn, err := wsock.Dial(ctx, endpoint, hdr, wsock.Options{Subprotocol: t.WS.Subprotocol, MaxMessage: maxResponseBytes, TLSConfig: testTLS})
	if err != nil {
		return fail(ex, began, fmt.Errorf("voice-ws connection failed: %w", err))
	}
	defer conn.Close()
	// cancelSession tells the service to stop when the exchange fails midway.
	cancelSession := func() {
		if ev, err := render(cfg.CancelTemplate, defaultVoiceCancel, chunkIn); err == nil {
			cctx, c := context.WithTimeout(context.Background(), 2*time.Second)
			defer c()
			b, _ := json.Marshal(ev)
			_ = conn.WriteText(cctx, b)
		}
	}
	fatal := func(err error) (model.Exchange, error) {
		cancelSession()
		return fail(ex, began, err)
	}
	send := func(ev map[string]any) error {
		b, _ := json.Marshal(ev)
		if len(b) > maxVoiceEventBytes+cfg.ChunkBytes*2 {
			return errors.New("voice-ws event exceeds its size limit")
		}
		return conn.WriteText(ctx, b)
	}
	if err := send(start); err != nil {
		return fatal(fmt.Errorf("voice-ws start failed: %w", err))
	}
	for off := 0; off < len(audio); off += cfg.ChunkBytes {
		hi := min(off+cfg.ChunkBytes, len(audio))
		c := chunkIn
		c.Extra = map[string]string{"audio_chunk_base64": base64.StdEncoding.EncodeToString(audio[off:hi])}
		ev, err := render(cfg.ChunkTemplate, defaultVoiceChunk, c)
		if err != nil {
			return fatal(fmt.Errorf("chunk template: %w", err))
		}
		if err := send(ev); err != nil {
			return fatal(fmt.Errorf("voice-ws audio failed: %w", err))
		}
	}
	if err := send(end); err != nil {
		return fatal(fmt.Errorf("voice-ws end failed: %w", err))
	}

	var (
		transcript, perceived strings.Builder
		replyAudio            []byte
		audioChunks, events   int
		actions               []model.Action
		redacted              []any
		redactedSize          = 2
		done                  bool
	)
	for events < cfg.MaxEvents && !done {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("voice-ws response timed out")
			} else {
				err = fmt.Errorf("voice-ws response failed: %w", err)
			}
			return fatal(err)
		}
		events++
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			return fatal(errors.New("voice-ws reply event must be JSON"))
		}
		obj, isObj := doc.(map[string]any)
		if !isObj {
			return fatal(errors.New("voice-ws reply event must be an object"))
		}
		if v, ok := lookup(doc, orDefault(t.ResponsePath, "text")); ok {
			if s, isStr := v.(string); isStr {
				transcript.WriteString(s)
			}
		}
		if v, ok := lookup(doc, orDefault(t.PerceivedPath, "perceived")); ok {
			if s, isStr := v.(string); isStr {
				perceived.WriteString(s)
			}
		}
		shown := obj
		if cfg.ReplyAudioPath != "" {
			if v, ok := lookup(doc, cfg.ReplyAudioPath); ok {
				s, isStr := v.(string)
				if !isStr {
					return fatal(errors.New("voice-ws reply audio path did not resolve to base64 text"))
				}
				chunk, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return fatal(errors.New("voice-ws reply audio is not valid base64"))
				}
				if len(replyAudio)+len(chunk) > maxVoiceAudioBytes {
					return fatal(errors.New("voice-ws reply audio exceeds 6 MiB"))
				}
				replyAudio = append(replyAudio, chunk...)
				audioChunks++
				shown = replacePath(obj, cfg.ReplyAudioPath, fmt.Sprintf("<audio chunk omitted: %d bytes>", len(chunk)))
			}
		}
		_, acts, _ := Parse(string(data), "text", t.ActionsPath, "perceived")
		for _, a := range acts {
			if len(actions) < 100 {
				actions = append(actions, a)
			}
		}
		if sz := len(data); redactedSize+sz <= maxResponseBytes {
			redacted = append(redacted, shown)
			redactedSize += sz
		}
		if v, ok := lookup(doc, cfg.DonePath); ok && asString(v) == cfg.DoneValue {
			done = true
		}
	}
	if !done {
		return fatal(errors.New("voice-ws event limit reached before completion"))
	}
	ex.DurationMs = int(time.Since(began).Milliseconds())
	raw, _ := json.Marshal(redacted)
	ex.RawResponse = string(raw)
	ex.ResponseText = transcript.String()
	ex.Perceived = perceived.String()
	ex.Actions = actions
	ex.ReplyAudio = replyAudio
	ex.Wire["events"] = events
	ex.Wire["reply_audio_chunks"] = audioChunks
	ex.Wire["completed"] = true
	return ex, nil
}

// replacePath returns a copy of obj with the value at the dot path replaced.
func replacePath(obj map[string]any, path, with string) map[string]any {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		out[k] = v
	}
	parts := strings.Split(path, ".")
	cur := out
	for i, p := range parts {
		if i == len(parts)-1 {
			if _, ok := cur[p]; ok {
				cur[p] = with
			}
			return out
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			return out
		}
		cp := make(map[string]any, len(next))
		for k, v := range next {
			cp[k] = v
		}
		cur[p] = cp
		cur = cp
	}
	return out
}
