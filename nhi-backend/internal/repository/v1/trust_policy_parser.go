package v1

import (
	"regexp"
	"sort"
	"strings"
)

// trustStatement is an internal, flattened representation of one IAM trust
// policy statement, built by parsing the standard AWS policy document JSON
// shape: {"Version":"...","Statement":[{"Effect":...,"Principal":...,
// "Action":...,"Condition":{...}}]}. This is AWS's own standardized format
// (not an app-specific schema), so parsing it precisely in Go is safe.
type trustStatement struct {
	Effect         string
	PrincipalType  string // "AWS" | "Service" | "Federated" | "*" | ""
	PrincipalValue string
	Action         string
	HasCondition   bool
	ConditionDesc  string // human-readable summary of the first condition, if any
}

var (
	awsARNAccountRe = regexp.MustCompile(`^arn:aws[a-z-]*:iam::(\d{12}):`)
	awsAccountIDRe  = regexp.MustCompile(`^\d{12}$`)
)

// principalAccountID extracts the 12-digit AWS account id from an IAM
// principal value. Accepts a full ARN or a bare 12-digit account id.
// Returns "" for "*", service principals, or anything else without an
// extractable account id.
func principalAccountID(principal string) string {
	p := strings.TrimSpace(principal)
	if m := awsARNAccountRe.FindStringSubmatch(p); m != nil {
		return m[1]
	}
	if awsAccountIDRe.MatchString(p) {
		return p
	}
	return ""
}

// parseTrustStatements decodes the "Statement" array out of a raw trust
// policy document (already json.Unmarshal'd into map[string]interface{}).
func parseTrustStatements(rawPolicy map[string]interface{}) []trustStatement {
	var out []trustStatement
	if rawPolicy == nil {
		return out
	}
	rawStmts, ok := rawPolicy["Statement"].([]interface{})
	if !ok {
		return out
	}

	for _, s := range rawStmts {
		stmtMap, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		st := trustStatement{}

		if v, ok := stmtMap["Effect"].(string); ok {
			st.Effect = v
		}

		switch action := stmtMap["Action"].(type) {
		case string:
			st.Action = action
		case []interface{}:
			parts := make([]string, 0, len(action))
			for _, a := range action {
				if s, ok := a.(string); ok {
					parts = append(parts, s)
				}
			}
			st.Action = strings.Join(parts, ", ")
		}

		switch principal := stmtMap["Principal"].(type) {
		case string:
			st.PrincipalType = "AWS"
			st.PrincipalValue = principal
		case map[string]interface{}:
			pTypes := make([]string, 0, len(principal))
			for k := range principal {
				pTypes = append(pTypes, k)
			}
			// Prefer "AWS" first for deterministic ordering, then alphabetical.
			sort.Slice(pTypes, func(i, j int) bool {
				if (pTypes[i] == "AWS") != (pTypes[j] == "AWS") {
					return pTypes[i] == "AWS"
				}
				return pTypes[i] < pTypes[j]
			})
			if len(pTypes) > 0 {
				pType := pTypes[0]
				st.PrincipalType = pType
				switch v := principal[pType].(type) {
				case string:
					st.PrincipalValue = v
				case []interface{}:
					vals := make([]string, 0, len(v))
					for _, item := range v {
						if s, ok := item.(string); ok {
							vals = append(vals, s)
						}
					}
					st.PrincipalValue = strings.Join(vals, ", ")
				}
			}
		}

		if condMap, ok := stmtMap["Condition"].(map[string]interface{}); ok && len(condMap) > 0 {
			st.HasCondition = true
			// Summarize just the first operator/key/value for a human-readable string.
		outer:
			for operator, keyVals := range condMap {
				if kvMap, ok := keyVals.(map[string]interface{}); ok {
					for key, val := range kvMap {
						valStr := ""
						switch v := val.(type) {
						case string:
							valStr = v
						case []interface{}:
							parts := make([]string, 0, len(v))
							for _, item := range v {
								if s, ok := item.(string); ok {
									parts = append(parts, s)
								}
							}
							valStr = strings.Join(parts, ", ")
						}
						st.ConditionDesc = operator + ":" + key + "=" + valStr
						break outer
					}
				}
			}
		}

		if st.PrincipalType == "" && st.PrincipalValue == "" && st.Effect == "" {
			continue
		}
		out = append(out, st)
	}
	return out
}

// trustRisk summarizes the cross-cutting risk flags derivable from the
// parsed statements, mirroring the precise per-statement logic already
// used elsewhere in this codebase for single-record lookups (as opposed to
// the cheaper SQL regex approximation used by the paginated credentials
// list endpoint).
type trustRisk struct {
	HasWildcardPrincipal bool
	HasExternalAccount   bool
	AnyAllowWithoutCond  bool
}

func computeTrustRisk(statements []trustStatement, credentialAccountID string) trustRisk {
	var risk trustRisk
	for _, st := range statements {
		if st.Effect != "Allow" {
			continue
		}
		pv := st.PrincipalValue
		if pv == "*" || strings.Contains(pv, "*") {
			risk.HasWildcardPrincipal = true
		}
		if !st.HasCondition {
			risk.AnyAllowWithoutCond = true
		}
		if st.PrincipalType == "AWS" {
			for _, part := range strings.Split(pv, ",") {
				acct := principalAccountID(strings.TrimSpace(part))
				if acct == "" {
					// Can't resolve to an account — fail safe and treat as external.
					if strings.TrimSpace(part) != "" && strings.TrimSpace(part) != "*" {
						risk.HasExternalAccount = true
					}
					continue
				}
				if credentialAccountID == "" || acct != credentialAccountID {
					risk.HasExternalAccount = true
				}
			}
		}
	}
	return risk
}
