package connector

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/guard"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
)

// CheckProduction applies the rules that only production targets get. The
// engine calls it before every run and the server calls it on save, so a
// target stored before the rule existed is still refused.
//
// A production target must be reached over an encrypted channel unless it is
// on this machine: https, wss, mqtts, tcp with tls on. Plain SIP is refused
// off-host because this build has no SIP TLS. Acknowledgement per run and
// single-file execution are enforced by the engine.
func CheckProduction(t model.Target) error {
	if t.Environment != "production" {
		return nil
	}
	check := func(label, endpoint string) error {
		if strings.TrimSpace(endpoint) == "" {
			return nil
		}
		if strings.HasPrefix(endpoint, "sip:") {
			host := endpoint[strings.Index(endpoint, "@")+1:]
			host = strings.SplitN(strings.SplitN(host, ";", 2)[0], ":", 2)[0]
			if !guard.IsLoopbackHost(host) {
				return fmt.Errorf("production %s: plain SIP is not allowed off this machine (this build has no SIP over TLS)", label)
			}
			return nil
		}
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" {
			return nil // the per-transport validation reports a malformed URL
		}
		if u.Scheme == "tcp" {
			if !t.TCP.TLS && !guard.IsLoopbackHost(u.Hostname()) {
				return fmt.Errorf("production %s: raw TCP off this machine needs tls on", label)
			}
			return nil
		}
		if err := guard.RequireTLSOffHost(u); err != nil {
			return fmt.Errorf("production %s: %w", label, err)
		}
		return nil
	}
	for _, e := range []struct{ label, url string }{
		{"endpoint", t.Endpoint}, {"direct endpoint", t.DirectEndpoint}, {"trigger endpoint", t.TriggerEndpoint},
		{"bridge stop endpoint", t.Bridge.StopEndpoint}, {"observer URL", t.Observer.URL},
	} {
		if err := check(e.label, e.url); err != nil {
			return err
		}
	}
	return nil
}
