package connector

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
)

// takeReplyAudio moves base64 audio at a dot path out of a JSON reply and into
// ex.ReplyAudio, and returns the reply with the audio replaced by a marker so
// the stored response stays readable. An unusable reply is returned unchanged.
func takeReplyAudio(path, raw string, ex *model.Exchange) string {
	var doc map[string]any
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return raw
	}
	v, ok := lookup(doc, path)
	s, isStr := v.(string)
	if !ok || !isStr || s == "" {
		return raw
	}
	audio, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(audio) > maxVoiceAudioBytes {
		return raw
	}
	ex.ReplyAudio = audio
	out := replacePath(doc, path, fmt.Sprintf("<audio omitted: %d bytes>", len(audio)))
	b, _ := json.Marshal(out)
	return strings.ToValidUTF8(string(b), "�")
}
