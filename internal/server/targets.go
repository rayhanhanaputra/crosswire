package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/connector"
	"github.com/rayhanhanaputra/crosswire/internal/guard"
	"github.com/rayhanhanaputra/crosswire/internal/model"
	"github.com/rayhanhanaputra/crosswire/internal/store"
)

var (
	envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	slugRe    = regexp.MustCompile(`[^a-z0-9]+`)
)

func (s *Server) listTargets(w http.ResponseWriter, r *http.Request) {
	ts, err := s.st.ListTargets()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if ts == nil {
		ts = []model.Target{}
	}
	writeJSON(w, 200, ts)
}

func (s *Server) getTarget(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.GetTarget(r.PathValue("id"))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) createTarget(w http.ResponseWriter, r *http.Request) {
	t := model.Target{Enabled: true}
	if !decode(w, r, &t) {
		return
	}
	if err := normalizeTarget(&t); err != nil {
		writeErr(w, 400, err)
		return
	}
	t.ID = newTargetID(t.Name)
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	if err := s.st.PutTarget(t); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 201, t)
}

func (s *Server) updateTarget(w http.ResponseWriter, r *http.Request) {
	old, err := s.st.GetTarget(r.PathValue("id"))
	if err != nil {
		storeErr(w, err)
		return
	}
	t := model.Target{Enabled: true}
	if !decode(w, r, &t) {
		return
	}
	if err := normalizeTarget(&t); err != nil {
		writeErr(w, 400, err)
		return
	}
	t.ID, t.CreatedAt, t.UpdatedAt = old.ID, old.CreatedAt, time.Now().UTC()
	if err := s.st.PutTarget(t); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) deleteTarget(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteTarget(r.PathValue("id")); err != nil {
		storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func storeErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, errors.New("not found"))
		return
	}
	writeErr(w, 500, err)
}

func newTargetID(name string) string {
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 32 {
		slug = strings.Trim(slug[:32], "-")
	}
	if slug == "" {
		slug = "target"
	}
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return slug + "-" + hex.EncodeToString(b)
}

func absoluteHTTP(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// pathEndpoint reports whether raw can be the endpoint of a direct or trigger
// path: http, https, ws, wss or tcp, with no credentials or fragment.
func pathEndpoint(raw string) bool {
	_, err := guard.ParseURL(raw, "http", "https", "ws", "wss", "tcp")
	return err == nil
}

// normalizeTarget validates a target in place and applies defaults.
func normalizeTarget(t *model.Target) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return errors.New("name is required")
	}
	if t.Environment != "staging" && t.Environment != "production" {
		return errors.New(`environment must be "staging" or "production"`)
	}
	t.Endpoint = strings.TrimSpace(t.Endpoint)
	if err := normalizeTransport(t); err != nil {
		return err
	}
	t.DirectEndpoint = strings.TrimSpace(t.DirectEndpoint)
	if t.DirectEndpoint != "" && !pathEndpoint(t.DirectEndpoint) {
		return errors.New("direct_endpoint must be an absolute http(s), ws(s) or tcp URL without credentials")
	}
	t.TriggerEndpoint = strings.TrimSpace(t.TriggerEndpoint)
	if t.TriggerEndpoint != "" && !pathEndpoint(t.TriggerEndpoint) {
		return errors.New("trigger_endpoint must be an absolute http(s), ws(s) or tcp URL without credentials")
	}
	t.Method = strings.ToUpper(strings.TrimSpace(t.Method))
	if t.Method == "" {
		t.Method = http.MethodPost
	}
	switch t.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return fmt.Errorf("method %s is not supported; use POST, PUT or PATCH", t.Method)
	}
	if t.TimeoutSeconds < 0 || t.TimeoutSeconds > 300 {
		return errors.New("timeout_seconds must be between 0 and 300")
	}
	if t.TimeoutSeconds == 0 {
		t.TimeoutSeconds = 30
	}
	if len(t.Headers) > 30 || len(t.SecretHeaders) > 10 {
		return errors.New("use at most 30 static headers and 10 secret headers")
	}
	for header, value := range t.Headers {
		if err := guard.CheckStaticHeader(header, value); err != nil {
			return err
		}
	}
	for header, env := range t.SecretHeaders {
		if err := guard.CheckSecretHeaderName(header); err != nil {
			return err
		}
		if !envNameRe.MatchString(env) {
			return fmt.Errorf("secret_headers[%s] must be the NAME of an environment variable, not the secret itself", header)
		}
	}
	if err := normalizeObserver(t); err != nil {
		return err
	}
	if err := normalizeMappers(t); err != nil {
		return err
	}
	if err := connector.CheckProduction(*t); err != nil {
		return err
	}
	t.Evidence.ForbiddenTools = clean(t.Evidence.ForbiddenTools)
	t.Evidence.ResponseMarkers = clean(t.Evidence.ResponseMarkers)
	for tool, args := range t.Evidence.ArgumentLimits {
		if len(args) == 0 {
			delete(t.Evidence.ArgumentLimits, tool)
		}
	}
	return nil
}

func clean(in []string) []string {
	out := []string{}
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
