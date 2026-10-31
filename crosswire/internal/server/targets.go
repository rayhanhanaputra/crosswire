package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/store"
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
	t.Transport = strings.ToLower(strings.TrimSpace(t.Transport))
	switch t.Transport {
	case "", "http":
		t.Transport = ""
		if !absoluteHTTP(t.Endpoint) {
			return errors.New("endpoint must be an absolute http(s) URL")
		}
	case "sip":
		if !strings.HasPrefix(t.Endpoint, "sip:") || !strings.Contains(t.Endpoint, "@") {
			return errors.New("a sip target's endpoint must be a sip:user@host[:port] URI")
		}
		if t.SIP.PasswordEnv != "" && !envNameRe.MatchString(t.SIP.PasswordEnv) {
			return errors.New("sip.password_env must be the NAME of an environment variable, not the password")
		}
		if t.SIP.LocalIP != "" && net.ParseIP(t.SIP.LocalIP) == nil {
			return errors.New("sip.local_ip must be an IP address")
		}
		if t.SIP.ListenSeconds < 0 || t.SIP.ListenSeconds > 120 || t.SIP.RingSeconds < 0 || t.SIP.RingSeconds > 120 {
			return errors.New("sip ring_seconds and listen_seconds must be between 0 and 120")
		}
	default:
		return fmt.Errorf("transport %q is not supported; use http or sip", t.Transport)
	}
	t.DirectEndpoint = strings.TrimSpace(t.DirectEndpoint)
	if t.DirectEndpoint != "" && !absoluteHTTP(t.DirectEndpoint) {
		return errors.New("direct_endpoint must be an absolute http(s) URL")
	}
	t.TriggerEndpoint = strings.TrimSpace(t.TriggerEndpoint)
	if t.TriggerEndpoint != "" && !absoluteHTTP(t.TriggerEndpoint) {
		return errors.New("trigger_endpoint must be an absolute http(s) URL")
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
	for header, env := range t.SecretHeaders {
		if !envNameRe.MatchString(env) {
			return fmt.Errorf("secret_headers[%s] must be the NAME of an environment variable, not the secret itself", header)
		}
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
