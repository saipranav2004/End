package v1

import (
	"net"
	"strings"

	repov1 "nhi-backend-api/internal/repository/v1"
)

// ResolveInput carries the ARN-ish fields of one CloudTrail event.
type ResolveInput struct {
	CallerARN        string // caller_arn: user ARN, assumed-role session ARN, or an AWS service name
	SessionIssuerARN string // session_issuer_arn: the role behind a session ("" for IAM users)
	AccessKeyID      string // access_key_id
	SourceIP         string // source_ip (may be a service name rather than an IP)
}

// Match is the resolver's answer. It is empty when nothing matched; callers
// must keep the event and show the raw ARN in that case.
type Match struct {
	IdentityID           string
	CredentialID         string
	IdentityTerminated   bool
	CredentialTerminated bool
	// Reason is the rule that found the match: session_issuer, caller_arn,
	// assumed_role, access_key, session_name or source_ip.
	Reason string
	// Ambiguous is set when a credential was found but several identities
	// share it and none could be singled out.
	Ambiguous bool
}

// Matched reports whether an identity or a credential was found.
func (m Match) Matched() bool { return m.IdentityID != "" || m.CredentialID != "" }

// IdentityResolver maps CloudTrail callers onto Postgres identities and
// credentials. It is built once per request from a bulk load and then answers
// every lookup from memory.
type IdentityResolver struct {
	credByARN       map[string]*repov1.LookupCredential
	credByName      map[string]*repov1.LookupCredential
	credByAccessKey map[string]*repov1.LookupCredential
	credByRoleKey   map[string][]*repov1.LookupCredential
	identByARN      map[string]*repov1.LookupIdentity
	identByIP       map[string][]*repov1.LookupIdentity
	identities      []*repov1.LookupIdentity
	identsByCred    map[string][]*repov1.LookupIdentity
	credsByIdent    map[string][]*repov1.LookupCredential
}

func NewIdentityResolver(l *repov1.IdentityLookup) *IdentityResolver {
	r := &IdentityResolver{
		credByARN:       map[string]*repov1.LookupCredential{},
		credByName:      map[string]*repov1.LookupCredential{},
		credByAccessKey: map[string]*repov1.LookupCredential{},
		credByRoleKey:   map[string][]*repov1.LookupCredential{},
		identByARN:      map[string]*repov1.LookupIdentity{},
		identByIP:       map[string][]*repov1.LookupIdentity{},
		identsByCred:    map[string][]*repov1.LookupIdentity{},
		credsByIdent:    map[string][]*repov1.LookupCredential{},
	}
	if l == nil {
		return r
	}
	credByID := map[string]*repov1.LookupCredential{}
	for i := range l.Credentials {
		c := &l.Credentials[i]
		credByID[c.ID] = c
		if c.ARN != "" {
			r.credByARN[c.ARN] = c
			if k := roleKeyFromRoleARN(c.ARN); k != "" {
				r.credByRoleKey[k] = append(r.credByRoleKey[k], c)
			}
		}
		if c.Name != "" {
			r.credByName[c.Name] = c
			if strings.EqualFold(c.Type, "access_key") {
				r.credByAccessKey[c.Name] = c
			}
		}
	}
	identByID := map[string]*repov1.LookupIdentity{}
	for i := range l.Identities {
		id := &l.Identities[i]
		identByID[id.ID] = id
		r.identities = append(r.identities, id)
		if id.ARN != "" {
			r.identByARN[id.ARN] = id
		}
		if id.PrivateIP != "" {
			r.identByIP[id.PrivateIP] = append(r.identByIP[id.PrivateIP], id)
		}
	}
	seen := map[[2]string]bool{}
	for _, b := range l.Bindings {
		id, c := identByID[b.IdentityID], credByID[b.CredentialID]
		if id == nil || c == nil || seen[[2]string{b.IdentityID, b.CredentialID}] {
			continue
		}
		seen[[2]string{b.IdentityID, b.CredentialID}] = true
		r.identsByCred[c.ID] = append(r.identsByCred[c.ID], id)
		r.credsByIdent[id.ID] = append(r.credsByIdent[id.ID], c)
	}
	return r
}

// Resolve finds the identity and credential behind one event, in this order:
// the session issuer, then the caller ARN, then the access key, and finally
// identity-only fallbacks (session name, private IP). It never guesses: a
// fallback is used only when it points at exactly one identity.
func (r *IdentityResolver) Resolve(in ResolveInput) Match {
	sess := parseAssumedRole(in.CallerARN)

	var cred *repov1.LookupCredential
	reason := ""
	if in.SessionIssuerARN != "" {
		if cred = r.credByARN[in.SessionIssuerARN]; cred == nil {
			cred = uniqueCred(r.credByRoleKey[roleKeyFromRoleARN(in.SessionIssuerARN)])
		}
		reason = "session_issuer"
	}
	if cred == nil && in.CallerARN != "" {
		if cred = r.credByARN[in.CallerARN]; cred == nil {
			cred = r.credByName[in.CallerARN]
		}
		reason = "caller_arn"
		if cred == nil && sess.ok {
			cred = uniqueCred(r.credByRoleKey[sess.account+":"+sess.role])
			reason = "assumed_role"
		}
	}
	if cred == nil && in.AccessKeyID != "" {
		cred = r.credByAccessKey[in.AccessKeyID]
		reason = "access_key"
	}

	if cred != nil {
		return r.fromCredential(cred, reason, sess, in)
	}
	return r.fromIdentityOnly(sess, in)
}

func (r *IdentityResolver) fromCredential(cred *repov1.LookupCredential, reason string, sess assumedRole, in ResolveInput) Match {
	m := Match{CredentialID: cred.ID, CredentialTerminated: cred.Terminated, Reason: reason}
	cands := r.identsByCred[cred.ID]
	var pick *repov1.LookupIdentity
	switch len(cands) {
	case 0:
		// No binding row for this credential (common: most roles have none).
		// Fall back to the caller itself, then the session name, then the IP.
		pick = r.identByARN[in.CallerARN]
		if pick == nil {
			pick = r.identityOnly(sess, in)
		}
	case 1:
		pick = cands[0]
	default:
		pick = r.disambiguate(cands, sess, in)
		m.Ambiguous = pick == nil
	}
	setIdentity(&m, pick)
	return m
}

func (r *IdentityResolver) fromIdentityOnly(sess assumedRole, in ResolveInput) Match {
	if id := r.identByARN[in.CallerARN]; id != nil {
		m := Match{Reason: "caller_arn"}
		setIdentity(&m, id)
		r.fillSoleCredential(&m, id)
		return m
	}
	var m Match
	if sess.ok {
		matches := filterIdentities(r.identities, func(i *repov1.LookupIdentity) bool { return matchesSession(i, sess.session) })
		switch len(matches) {
		case 0:
		case 1:
			m.Reason = "session_name"
			setIdentity(&m, matches[0])
			r.fillSoleCredential(&m, matches[0])
			return m
		default:
			m.Ambiguous = true
		}
	}
	if id := uniqueByPrivateIP(r.identByIP, in.SourceIP, nil); id != nil {
		m.Reason, m.Ambiguous = "source_ip", false
		setIdentity(&m, id)
		r.fillSoleCredential(&m, id)
	}
	return m
}

// identityOnly finds a single identity from the session name, else from a
// private source IP, across all loaded identities.
func (r *IdentityResolver) identityOnly(sess assumedRole, in ResolveInput) *repov1.LookupIdentity {
	if sess.ok {
		if m := filterIdentities(r.identities, func(i *repov1.LookupIdentity) bool { return matchesSession(i, sess.session) }); len(m) == 1 {
			return m[0]
		}
	}
	return uniqueByPrivateIP(r.identByIP, in.SourceIP, nil)
}

func (r *IdentityResolver) disambiguate(cands []*repov1.LookupIdentity, sess assumedRole, in ResolveInput) *repov1.LookupIdentity {
	if id := r.identByARN[in.CallerARN]; id != nil && containsIdentity(cands, id) {
		return id
	}
	if sess.ok {
		if m := filterIdentities(cands, func(i *repov1.LookupIdentity) bool { return matchesSession(i, sess.session) }); len(m) == 1 {
			return m[0]
		}
	}
	return uniqueByPrivateIP(r.identByIP, in.SourceIP, cands)
}

func (r *IdentityResolver) fillSoleCredential(m *Match, id *repov1.LookupIdentity) {
	if creds := r.credsByIdent[id.ID]; len(creds) == 1 {
		m.CredentialID, m.CredentialTerminated = creds[0].ID, creds[0].Terminated
	}
}

func setIdentity(m *Match, id *repov1.LookupIdentity) {
	if id != nil {
		m.IdentityID, m.IdentityTerminated = id.ID, id.Terminated
	}
}

func uniqueCred(cs []*repov1.LookupCredential) *repov1.LookupCredential {
	if len(cs) == 1 {
		return cs[0]
	}
	return nil
}

// matchesSession reports whether an identity is the thing a role session was
// named after. Session names are usually the tail of the resource ARN (an EC2
// instance ID, a function name), so an ends-with match is needed.
func matchesSession(i *repov1.LookupIdentity, session string) bool {
	if session == "" {
		return false
	}
	return i.Name == session || i.ARN == session ||
		strings.HasSuffix(i.ARN, "/"+session) || strings.HasSuffix(i.ARN, ":"+session)
}

// uniqueByPrivateIP matches only private addresses (public IPs and service
// names in source_ip say nothing about a single host) and only when exactly
// one identity, optionally within cands, owns the address.
func uniqueByPrivateIP(byIP map[string][]*repov1.LookupIdentity, ip string, cands []*repov1.LookupIdentity) *repov1.LookupIdentity {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || !parsed.IsPrivate() {
		return nil
	}
	matches := byIP[parsed.String()]
	if cands != nil {
		matches = filterIdentities(matches, func(i *repov1.LookupIdentity) bool { return containsIdentity(cands, i) })
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return nil
}

func filterIdentities(in []*repov1.LookupIdentity, keep func(*repov1.LookupIdentity) bool) []*repov1.LookupIdentity {
	var out []*repov1.LookupIdentity
	for _, i := range in {
		if keep(i) {
			out = append(out, i)
		}
	}
	return out
}

func containsIdentity(list []*repov1.LookupIdentity, id *repov1.LookupIdentity) bool {
	for _, i := range list {
		if i == id {
			return true
		}
	}
	return false
}

type assumedRole struct {
	ok      bool
	account string
	role    string
	session string
}

// parseAssumedRole splits arn:aws:sts::123:assumed-role/my-role/session-name.
// Anything else (IAM users, service names) returns ok=false.
func parseAssumedRole(arn string) assumedRole {
	const marker = ":assumed-role/"
	idx := strings.Index(arn, marker)
	if idx < 0 {
		return assumedRole{}
	}
	parts := strings.SplitN(arn[idx+len(marker):], "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return assumedRole{}
	}
	fields := strings.Split(arn[:idx], ":")
	account := fields[len(fields)-1]
	if account == "" {
		return assumedRole{}
	}
	return assumedRole{ok: true, account: account, role: parts[0], session: parts[1]}
}

// roleKeyFromRoleARN returns "account:roleName" for arn:aws:iam::123:role/path/name.
// The path is dropped because session ARNs do not carry it.
func roleKeyFromRoleARN(arn string) string {
	const marker = ":role/"
	idx := strings.Index(arn, marker)
	if idx < 0 {
		return ""
	}
	rest := arn[idx+len(marker):]
	name := rest[strings.LastIndex(rest, "/")+1:]
	fields := strings.Split(arn[:idx], ":")
	account := fields[len(fields)-1]
	if name == "" || account == "" {
		return ""
	}
	return account + ":" + name
}
