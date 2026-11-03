package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/guard"
	"github.com/rayhanhanaputra/crosswire/internal/model"
	"github.com/rayhanhanaputra/crosswire/internal/wsock"
)

const maxWSRequestBytes = 12 << 20

// fail records an error on the exchange the way the HTTP connector does.
func fail(ex model.Exchange, start time.Time, err error) (model.Exchange, error) {
	ex.DurationMs = int(time.Since(start).Milliseconds())
	ex.Error = err.Error()
	return ex, err
}

func wsHeaders(t model.Target) (http.Header, error) {
	h := http.Header{}
	headers, err := StaticAndSecretHeaders(t)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		h.Set(k, v)
	}
	return h, nil
}

// sendWS sends the rendered request as one text message and takes the first
// reply message as the response.
func sendWS(ctx context.Context, t model.Target, endpoint string, body map[string]any, in Input) (model.Exchange, error) {
	ex := model.Exchange{Request: body}
	start := time.Now()
	if _, err := guard.ParseURL(endpoint, "ws", "wss"); err != nil {
		return fail(ex, start, fmt.Errorf("websocket target: %w", err))
	}
	payload, _ := json.Marshal(body)
	if len(payload) > maxWSRequestBytes {
		return fail(ex, start, errors.New("rendered request exceeds 12 MiB"))
	}
	hdr, err := wsHeaders(t)
	if err != nil {
		return fail(ex, start, err)
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout(t))
	defer cancel()
	conn, err := wsock.Dial(ctx, endpoint, hdr, wsock.Options{Subprotocol: t.WS.Subprotocol, MaxMessage: maxResponseBytes, TLSConfig: testTLS})
	if err != nil {
		return fail(ex, start, fmt.Errorf("websocket target connection failed: %w", err))
	}
	defer conn.Close()
	ex.Wire = map[string]any{"transport": "ws", "message_type": "text"}
	if t.WS.Subprotocol != "" {
		ex.Wire["subprotocol"] = t.WS.Subprotocol
	}
	if err := conn.WriteText(ctx, payload); err != nil {
		return fail(ex, start, fmt.Errorf("websocket send failed: %w", err))
	}
	typ, data, err := conn.Read(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("websocket response timed out")
		} else {
			err = fmt.Errorf("websocket response failed: %w", err)
		}
		return fail(ex, start, err)
	}
	ex.DurationMs = int(time.Since(start).Milliseconds())
	if typ == wsock.Binary {
		ex.Wire["reply_type"] = "binary"
	} else {
		ex.Wire["reply_type"] = "text"
	}
	Finish(t, &ex, strings.ToValidUTF8(string(data), "�"))
	return ex, nil
}
