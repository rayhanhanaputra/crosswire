package targets

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/evidence"
)

// Provenance confirm (--confirm=provenance) decides a privileged call by
// where its argument values came from, not by which endpoint carried the
// request. A session keeps two lists of normalised text:
//
//   - untrusted: everything the agent perceived or read from storage for the
//     session (transcripts, OCR and VLM text, extracted documents, stored
//     handoff notes and mail);
//   - trusted: the user's chat turns and the results of record lookups.
//
// A call is refused when a string argument value occurs in untrusted text
// and in no trusted text. Values are compared as lower-case alphanumerics,
// the form evidence.Carries uses, and values shorter than minProvenanceLen
// are skipped because a one-letter value matches any text.
const (
	minProvenanceLen = 3
	provenanceReason = "provenance: argument came from untrusted text"
	maxLedgers       = 4096
	maxLedgerTexts   = 64
)

// recordTools return facts from the system of record. Their results are
// trusted, minus any text that merely echoes a call argument, so a model
// cannot launder an attacker string by looking it up.
var recordTools = map[string]bool{"lookup_vendor": true, "lookup_ticket": true, "read_calendar": true, "get_state": true}

type ledger struct {
	mu        sync.Mutex
	untrusted []string
	trusted   []string
}

type ledgerKey struct{}

func addText(list *[]string, text string) {
	n := evidence.Alnum(text)
	if n == "" || len(*list) >= maxLedgerTexts {
		return
	}
	*list = append(*list, n)
}

func (l *ledger) taint(text string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	addText(&l.untrusted, text)
}

func (l *ledger) trust(text string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	addText(&l.trusted, text)
}

// untrustedArg returns the name of the first string argument that came from
// untrusted text only, or "".
func (l *ledger) untrustedArg(args map[string]any) string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, ok := args[k].(string)
		if !ok {
			continue
		}
		n := evidence.Alnum(v)
		if len(n) < minProvenanceLen || !anyContains(l.untrusted, n) || anyContains(l.trusted, n) {
			continue
		}
		return k
	}
	return ""
}

func anyContains(list []string, needle string) bool {
	for _, t := range list {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

// ledgerFor returns the session's ledger, creating it on first use. A
// request with no session gets a private one, so nothing leaks between
// unrelated requests.
func (s *Server) ledgerFor(session string) *ledger {
	if session == "" {
		return &ledger{}
	}
	s.ledgerMu.Lock()
	defer s.ledgerMu.Unlock()
	if s.ledgers == nil {
		s.ledgers = map[string]*ledger{}
	}
	if l, ok := s.ledgers[session]; ok {
		return l
	}
	if len(s.ledgerOrder) >= maxLedgers {
		delete(s.ledgers, s.ledgerOrder[0])
		s.ledgerOrder = s.ledgerOrder[1:]
	}
	l := &ledger{}
	s.ledgers[session] = l
	s.ledgerOrder = append(s.ledgerOrder, session)
	return l
}

func ledgerOf(ctx context.Context) *ledger {
	l, _ := ctx.Value(ledgerKey{}).(*ledger)
	return l
}

// withSession puts the session and its ledger on ctx.
func (s *Server) withSession(ctx context.Context, session string) context.Context {
	ctx = context.WithValue(ctx, sessionKey{}, session)
	return context.WithValue(ctx, ledgerKey{}, s.ledgerFor(session))
}

// taint records text the agent is about to read from an untrusted path.
func taint(ctx context.Context, text string) { ledgerOf(ctx).taint(text) }

// trustResult records a record tool's result, minus echoed arguments.
func trustResult(ctx context.Context, tool string, args map[string]any, out string) {
	if !recordTools[tool] {
		return
	}
	for _, v := range args {
		if sv, ok := v.(string); ok && strings.TrimSpace(sv) != "" {
			out = strings.ReplaceAll(out, sv, " ")
		}
	}
	ledgerOf(ctx).trust(out)
}
