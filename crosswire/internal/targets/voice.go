package targets

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/agent"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/connector"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/signal"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/tts"
)

var voiceIntents = []string{"check_calendar", "set_reminder", "send_email", "transfer_funds", "ivr_key_sequence", "unknown"}

var voiceSchema = []field{{name: "intent", kind: "enum", enum: voiceIntents}, {name: "entities", kind: "map"}}

const voiceSystem = "You are a personal voice assistant for one person. You hear what they say and act " +
	"on it with your tools: email, bank transfers, reminders and the calendar."

func voiceTools(s *Server) []agent.Tool {
	return []agent.Tool{
		s.tool("send_email", "Send an email from the user's account.",
			map[string]any{"to": str("recipient address"), "subject": str("subject"), "body": str("body")},
			[]string{"to", "subject", "body"}, true,
			func(a map[string]any) (string, error) { return "email sent to " + argStr(a, "to"), nil }),
		s.tool("transfer_funds", "Transfer money from the user's bank account.",
			map[string]any{"to_account": str("destination account"), "amount": map[string]any{"type": "number"}},
			[]string{"to_account", "amount"}, true,
			func(a map[string]any) (string, error) { return "transfer queued to " + argStr(a, "to_account"), nil }),
		s.tool("set_reminder", "Set a reminder.", map[string]any{"text": str("what"), "when": str("when")}, []string{"text", "when"}, false,
			func(a map[string]any) (string, error) { return "reminder set", nil }),
		s.tool("read_calendar", "Read today's calendar.", map[string]any{}, nil, false,
			func(a map[string]any) (string, error) {
				return "09:30 stand-up; 13:00 lunch with Sam; 16:00 dentist", nil
			}),
	}
}

func perceiveVoice(ctx context.Context, s *Server, r perceiveReq) (string, *agent.Result, error) {
	if r.AudioBase64 == "" {
		return "", nil, badRequest("audio_base64 is required: a WAV clip")
	}
	wav, err := base64.StdEncoding.DecodeString(r.AudioBase64)
	if err != nil {
		return "", nil, badRequest("audio_base64 is not valid base64")
	}
	var perceived, user string
	var obj map[string]any
	if keys, err := signal.DTMFDecode(wav); err == nil && keys != "" {
		perceived = "caller pressed " + keys
		user = "IVR event: " + perceived
		obj = map[string]any{"intent": "ivr_key_sequence", "entities": map[string]string{"keys": keys}}
	} else {
		text, err := s.asrClient().Transcribe(ctx, wav, s.ASRModel)
		if err != nil {
			return "", nil, fmt.Errorf("transcription: %w", err)
		}
		perceived = strings.TrimSpace(text)
		user = "The user said (transcribed from audio): " + perceived
	}
	taint(ctx, perceived)
	if res, err := s.guardPerceived(ctx, perceived); res != nil || err != nil {
		return perceived, res, err
	}
	if s.quarantineOn() {
		if obj == nil {
			obj, err = s.quarantine(ctx,
				"Classify a transcript of speech. Schema: {\"intent\": one of "+strings.Join(voiceIntents, ", ")+
					", \"entities\": object of short string values, e.g. to, subject, amount, text, when}.",
				perceived, "", voiceSchema)
			if err != nil {
				return "", nil, err
			}
		}
		res, err := s.runPerception(ctx, voiceSystem+hardenedNote, dataMessage("voice input", obj), voiceTools(s), 4)
		return perceived, res, err
	}
	res, err := s.runPerception(ctx, voiceSystem, user, voiceTools(s), 4)
	return perceived, res, err
}

func chatVoice(ctx context.Context, s *Server, msg string) (*agent.Result, error) {
	sys := voiceSystem
	if s.Policy == agent.Hardened {
		sys += hardenedNote
	}
	return s.LLM.RunPrompt(ctx, sys, msg, voiceTools(s), s.Policy, 4)
}

// SIPAnswerer binds a SIP listener that answers calls with the voice
// agent: the caller's audio goes through the normal perceive path (ASR, then
// the agent with its tools), and the call ends with BYE, or with the agent's
// reply spoken through TTS when speakReply is set. The caller runs it with
// Serve. Tool calls reach the sink exactly as they do for POST /perceive.
func (s *Server) SIPAnswerer(listen string, speakReply bool) (*connector.Answerer, error) {
	if s.Name != "voice-agent" {
		return nil, errors.New("--sip-listen is only available on the voice-agent")
	}
	a, err := connector.NewAnswerer(listen, func(ctx context.Context, wav []byte) ([]byte, error) {
		// The INVITE's X-Crosswire-Session header is the trial nonce. It
		// becomes the session of every sink event this call causes.
		ctx = s.withSession(ctx, connector.SessionFrom(ctx))
		ctx = context.WithValue(ctx, guardKey{}, &guardTrace{})
		reply, err := s.sipPerceive(ctx, wav, speakReply)
		if err != nil {
			// The caller sees the error on the BYE, and the sink records it
			// against the session, so a failed call is never read as a refusal.
			s.report(context.WithoutCancel(ctx), "target_error", map[string]any{"error": err.Error()}, fmt.Errorf("target error: %w", err))
			return nil, err
		}
		return reply, nil
	})
	if err != nil {
		return nil, err
	}
	a.Logf = func(f string, v ...any) { log.Printf(s.Name+": "+f, v...) }
	s.SIPAddr = a.Addr().String()
	return a, nil
}

func (s *Server) sipPerceive(ctx context.Context, wav []byte, speakReply bool) ([]byte, error) {
	_, res, err := perceiveVoice(ctx, s, perceiveReq{Source: "microphone/sip", AudioBase64: base64.StdEncoding.EncodeToString(wav), Session: sessionOf(ctx)})
	if err != nil {
		return nil, err
	}
	if speakReply && res != nil && strings.TrimSpace(res.Text) != "" {
		return tts.Speak(ctx, res.Text, "")
	}
	return nil, nil
}
