package clickhouse

import (
	"context"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// Subject identifies whose events to look at: a set of keys, each an identity
// ARN, a credential (role) ARN or an access key ID. An event belongs to the
// subject when a key equals its caller_arn, session_issuer_arn or
// access_key_id. Blank and duplicate keys are dropped, because an empty value
// compared against the table would match every row that has an empty issuer or
// access key.
type Subject struct {
	Keys []string
	// IssuerOnly restricts matching to session_issuer_arn (used to list the
	// callers of a credential).
	IssuerOnly bool
}

// NewSubject builds a Subject from keys, dropping blanks and duplicates.
func NewSubject(keys ...string) Subject { return Subject{Keys: cleanKeys(keys)} }

func cleanKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func (s Subject) empty() bool { return len(cleanKeys(s.Keys)) == 0 }

// Scope bounds every query. Accounts is the tenant's account list and is the
// isolation boundary: with no accounts nothing is queried. Start/End are
// optional (zero means unbounded on that side). Service, when set, keeps only
// events of that AWS service (short name such as "s3").
type Scope struct {
	Accounts []string
	Start    time.Time
	End      time.Time
	Service  string
}

// buildFilter returns the WHERE body (without the WHERE keyword) and its bound
// parameters. ok is false when the query must not run (no subject keys or no
// accounts).
func buildFilter(sub Subject, sc Scope) (clause string, args []any, ok bool) {
	keys := cleanKeys(sub.Keys)
	if len(keys) == 0 || len(sc.Accounts) == 0 {
		return "", nil, false
	}
	if sub.IssuerOnly {
		clause = "session_issuer_arn IN {keys:Array(String)}"
	} else {
		clause = "(caller_arn IN {keys:Array(String)} OR session_issuer_arn IN {keys:Array(String)} OR access_key_id IN {keys:Array(String)})"
	}
	args = append(args, ch.Named("keys", keys))
	clause += " AND account_id IN {accounts:Array(String)}"
	args = append(args, ch.Named("accounts", sc.Accounts))
	if svc := strings.ToLower(strings.TrimSpace(sc.Service)); svc != "" {
		clause += " AND lower(event_source) IN {services:Array(String)}"
		args = append(args, ch.Named("services", []string{svc, svc + ".amazonaws.com"}))
	}
	tc, ta := timeFilter(sc)
	return clause + tc, append(args, ta...), true
}

func timeFilter(sc Scope) (string, []any) {
	var clause string
	var args []any
	if !sc.Start.IsZero() {
		clause += " AND event_time >= {start:DateTime64(3)}"
		args = append(args, ch.Named("start", sc.Start.UTC()))
	}
	if !sc.End.IsZero() {
		clause += " AND event_time <= {end:DateTime64(3)}"
		args = append(args, ch.Named("end", sc.End.UTC()))
	}
	return clause, args
}

// ServiceName turns a CloudTrail event source ("s3.amazonaws.com") into the
// short service name used elsewhere in the API ("s3").
func ServiceName(eventSource string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(eventSource)), ".amazonaws.com")
}

// withTimeout applies the per-query timeout from the client config.
func (c *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}
