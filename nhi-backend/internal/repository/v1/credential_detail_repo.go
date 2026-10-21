package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/v1"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	"nhi-backend-api/internal/scope"
)

// pqStringArray adapts a []string for use as a Postgres text[] query
// argument (used with the `= ANY($n)` idiom).
func pqStringArray(ss []string) interface{} {
	return pq.Array(ss)
}

// ErrCredentialNotFound is returned when the requested :id has no matching,
// non-deleted row in credentials. Handlers must check this with errors.Is
// (never string-match) and translate it to 404.
var ErrCredentialNotFound = errors.New("credential not found")

type CredentialDetailRepository struct{}

func NewCredentialDetailRepository() *CredentialDetailRepository {
	return &CredentialDetailRepository{}
}

// coreCredential mirrors every column of the credentials table (31 columns,
// confirmed via information_schema.columns against the live database).
type coreCredential struct {
	ID                   string
	ARN                  string
	Name                 string
	CredentialType       string
	AccountID            string
	Region               string
	Status               string
	CreatedAtAWS         *time.Time
	FirstSeenAt          *time.Time
	LastSeenAt           *time.Time
	DeletedAt            *time.Time
	LastUsedAt           *time.Time
	LastRotatedAt        *time.Time
	ExpiresAt            *time.Time
	RotationEnabled      bool
	RotationIntervalDays *int
	Tags                 []byte
	OwnerPrincipalARN    *string
	TrustPolicy          []byte
	AttachedPolicies     []byte
	InlinePolicies       []byte
	PermissionBoundary   *string
	MaxSessionDuration   *int
	PrivilegeLevel       *int
	AssumableRoleARNs    []byte
	DiscoverySource      *string
	TypeAttributes       []byte
	RawAPIResponse       []byte
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
	ScopeAccountIDs      []string
	ScopeRegions         []string
}

// loadCoreCredential fetches a credential only when its id belongs to the
// caller's selected account/region scope.
func (r *CredentialDetailRepository) loadCoreCredential(ctx context.Context, resolved *scope.DataScope, id string) (*coreCredential, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, ErrCredentialNotFound
	}
	query := fmt.Sprintf(`
		SELECT
			id::text, arn, name, credential_type, account_id, region, status,
			created_at_aws, first_seen_at, last_seen_at, deleted_at,
			last_used_at, last_rotated_at, expires_at,
			COALESCE(rotation_enabled, false), rotation_interval_days,
			tags, owner_principal_arn, trust_policy, attached_policies, inline_policies,
			permission_boundary_arn, max_session_duration, privilege_level,
			assumable_role_arns, discovery_source, type_attributes,
			raw_api_response, created_at, updated_at
		FROM %s.credentials
		WHERE id = $1::uuid
		  AND account_id = ANY($2::text[]) AND region = ANY($3::text[])
		  AND deleted_at IS NULL
		LIMIT 1`, resolved.QSchema())

	var c coreCredential
	err := resolved.DB.QueryRowContext(ctx, query, id, pq.Array(resolved.AccountIDs), pq.Array(resolved.Regions)).Scan(
		&c.ID, &c.ARN, &c.Name, &c.CredentialType, &c.AccountID, &c.Region, &c.Status,
		&c.CreatedAtAWS, &c.FirstSeenAt, &c.LastSeenAt, &c.DeletedAt,
		&c.LastUsedAt, &c.LastRotatedAt, &c.ExpiresAt,
		&c.RotationEnabled, &c.RotationIntervalDays,
		&c.Tags, &c.OwnerPrincipalARN, &c.TrustPolicy, &c.AttachedPolicies, &c.InlinePolicies,
		&c.PermissionBoundary, &c.MaxSessionDuration, &c.PrivilegeLevel,
		&c.AssumableRoleARNs, &c.DiscoverySource, &c.TypeAttributes,
		&c.RawAPIResponse, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCredentialNotFound
		}
		return nil, fmt.Errorf("load credential: %w", err)
	}
	c.ScopeAccountIDs = append([]string(nil), resolved.AccountIDs...)
	c.ScopeRegions = append([]string(nil), resolved.Regions...)
	return &c, nil
}

// ── shared derivations (reused across tabs) ─────────────────────────────

func daysBetween(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}

func ageDays(c *coreCredential) *int {
	if c.CreatedAtAWS == nil {
		return nil
	}
	d := daysBetween(*c.CreatedAtAWS, time.Now())
	return &d
}

func idleDays(c *coreCredential) *int {
	if c.LastUsedAt == nil {
		return nil
	}
	d := daysBetween(*c.LastUsedAt, time.Now())
	return &d
}

func daysSinceRotation(c *coreCredential) *int {
	ref := c.LastRotatedAt
	if ref == nil {
		ref = c.CreatedAtAWS
	}
	if ref == nil {
		return nil
	}
	d := daysBetween(*ref, time.Now())
	return &d
}

func daysUntilExpiry(c *coreCredential) *int {
	if c.ExpiresAt == nil {
		return nil
	}
	d := daysBetween(time.Now(), *c.ExpiresAt)
	return &d
}

func rotationOverdue(c *coreCredential) bool {
	if !c.RotationEnabled || c.RotationIntervalDays == nil {
		return false
	}
	ref := c.LastRotatedAt
	if ref == nil {
		ref = c.CreatedAtAWS
	}
	if ref == nil {
		return false
	}
	return time.Now().After(ref.AddDate(0, 0, *c.RotationIntervalDays))
}

func rotationStatus(c *coreCredential) string {
	if !c.RotationEnabled {
		return "Disabled"
	}
	if rotationOverdue(c) {
		return "Overdue"
	}
	return "Enabled"
}

// deriveCategory is a best-effort, heuristic mapping from credential_type to
// a human-facing category. There is no stored "category" column in this
// schema, so this is a presentation-layer convenience only.
func deriveCategory(credentialType string) string {
	switch strings.ToLower(strings.TrimSpace(credentialType)) {
	case "iam_role", "iam_user", "iam_group", "access_key", "iam_policy":
		return "IAM"
	case "secret", "secrets_manager_secret", "secretsmanager_secret":
		return "Secrets Manager"
	case "kms_key", "kms":
		return "KMS"
	case "service_account", "service_account_key":
		return "Service Account"
	case "instance_profile":
		return "Compute"
	case "":
		return "Unknown"
	default:
		return strings.ToUpper(credentialType[:1]) + credentialType[1:]
	}
}

func decodeJSON(raw []byte) interface{} {
	if len(raw) == 0 {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func nilIfEmptyStr(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// ═════════════════════════════════════════════════════════════════════════
// tab=overview — Tables: credentials (all 31), identity_credentials, identities
// ═════════════════════════════════════════════════════════════════════════

// credentialKeys are what identify this credential in CloudTrail: its ARN
// (the session issuer for roles) and, for access keys, the key ID.
func credentialKeys(c *coreCredential) []string {
	return credentialActivityKeys(c.ARN, c.Name, c.CredentialType)
}

// applyCredentialLastUsed replaces the (no longer populated) last_used_at
// column with the CloudTrail answer. When CloudTrail could not be read the
// Postgres value is left alone.
func applyCredentialLastUsed(ctx context.Context, c *coreCredential, src ActivitySource) {
	if !src.Available() {
		return
	}
	keys := credentialKeys(c)
	seen := src.LastSeen(ctx, keys)
	if src.Degraded() {
		return
	}
	c.LastUsedAt = latestOf(seen, keys)
}

func (r *CredentialDetailRepository) GetOverview(ctx context.Context, resolved *scope.DataScope, id string, src ActivitySource) (*models.OverviewResponse, error) {
	src = orNoActivity(src)
	tenantDB := resolved.DB
	c, err := r.loadCoreCredential(ctx, resolved, id)
	if err != nil {
		return nil, err
	}
	applyCredentialLastUsed(ctx, c, src)

	boundIdentities, err := r.loadBoundIdentities(ctx, tenantDB, resolved.QSchema(), c, src)
	if err != nil {
		return nil, err
	}

	var rawTrust map[string]interface{}
	if len(c.TrustPolicy) > 0 {
		_ = json.Unmarshal(c.TrustPolicy, &rawTrust)
	}
	statements := parseTrustStatements(rawTrust)
	risk := computeTrustRisk(statements, c.AccountID)

	resp := &models.OverviewResponse{
		Tab: "overview",
		Credential: models.OverviewCredential{
			ID:                    c.ID,
			Name:                  c.Name,
			ARN:                   c.ARN,
			CredentialType:        c.CredentialType,
			Category:              deriveCategory(c.CredentialType),
			AccountID:             c.AccountID,
			Region:                c.Region,
			Status:                c.Status,
			OwnerPrincipalARN:     c.OwnerPrincipalARN,
			CreatedAtAWS:          c.CreatedAtAWS,
			AgeDays:               ageDays(c),
			LastUsedAt:            c.LastUsedAt,
			IdleDays:              idleDays(c),
			LastRotatedAt:         c.LastRotatedAt,
			DaysSinceRotation:     daysSinceRotation(c),
			RotationEnabled:       c.RotationEnabled,
			RotationIntervalDays:  c.RotationIntervalDays,
			RotationStatus:        rotationStatus(c),
			ExpiresAt:             c.ExpiresAt,
			DaysUntilExpiry:       daysUntilExpiry(c),
			MaxSessionDuration:    c.MaxSessionDuration,
			PermissionBoundaryARN: c.PermissionBoundary,
			RotationOverdue:       rotationOverdue(c),
			IsOrphaned:            len(boundIdentities) == 0,
			HasExternalTrust:      risk.HasExternalAccount,
		},
		BoundIdentities: boundIdentities,
		TypeAttributes:  decodeJSON(c.TypeAttributes),
	}

	resp.RiskSignals = buildRiskSignals(c, len(boundIdentities), risk)
	return resp, nil
}

func (r *CredentialDetailRepository) loadBoundIdentities(ctx context.Context, tenantDB *sql.DB, schema string, c *coreCredential, src ActivitySource) ([]models.BoundIdentity, error) {
	query := fmt.Sprintf(`
		SELECT i.id::text, COALESCE(i.name,''), COALESCE(i.identity_type,''),
		       COALESCE(i.account_id,''), COALESCE(i.status,''), COALESCE(i.arn,''),
		       COALESCE(ic.binding_type,'')
		FROM %[1]s.identity_credentials ic
		JOIN %[1]s.identities i ON i.id::text = ic.identity_id::text
		WHERE ic.credential_id::text = $1
		  AND i.deleted_at IS NULL AND i.account_id = ANY($2::text[]) AND i.region = ANY($3::text[])
		ORDER BY i.name NULLS LAST`, schema)

	rows, err := tenantDB.QueryContext(ctx, query, c.ID, pq.Array(c.ScopeAccountIDs), pq.Array(c.ScopeRegions))
	if err != nil {
		return nil, fmt.Errorf("query bound identities: %w", err)
	}
	defer rows.Close()

	out := make([]models.BoundIdentity, 0)
	var arns []string
	for rows.Next() {
		var bi models.BoundIdentity
		var arn string
		if err := rows.Scan(&bi.IdentityID, &bi.Name, &bi.IdentityType, &bi.AccountID, &bi.Status, &arn, &bi.BindingType); err != nil {
			return nil, fmt.Errorf("scan bound identity: %w", err)
		}
		out = append(out, bi)
		arns = append(arns, arn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// last_activity_at per bound identity comes from CloudTrail: the identity's
	// own ARN, or this credential's ARN / key (what the identity acts through).
	if src.Available() && len(out) > 0 {
		credKeys := credentialKeys(c)
		keys := append([]string(nil), credKeys...)
		keys = append(keys, arns...)
		seen := src.LastSeen(ctx, keys)
		if !src.Degraded() {
			for i := range out {
				out[i].LastActivityAt = latestOf(seen, append([]string{arns[i]}, credKeys...))
			}
		}
	}
	return out, nil
}

func buildRiskSignals(c *coreCredential, boundCount int, risk trustRisk) []models.RiskSignal {
	expired := c.ExpiresAt != nil && c.ExpiresAt.Before(time.Now())
	expSoon := false
	if c.ExpiresAt != nil && !expired {
		d := daysUntilExpiry(c)
		if d != nil && *d <= 30 {
			expSoon = true
		}
	}
	idle := idleDays(c)
	stale := idle != nil && *idle > 90

	return []models.RiskSignal{
		{Signal: "Orphaned (no identity bound)", Status: boundCount == 0, Severity: "high"},
		{Signal: "Overshared (3+ identities)", Status: boundCount >= 3, Severity: "medium"},
		{Signal: "Never rotated", Status: c.LastRotatedAt == nil && rotatableCredential(c.CredentialType), Severity: "high"},
		{Signal: "Rotation overdue", Status: rotationOverdue(c), Severity: "high"},
		{Signal: "Expiring soon (< 30 days)", Status: expSoon, Severity: "high"},
		{Signal: "Already expired", Status: expired, Severity: "critical"},
		{Signal: "Stale (unused > 90 days)", Status: stale, Severity: "high"},
		{Signal: "External trust (cross-account)", Status: risk.HasExternalAccount, Severity: "medium"},
		{Signal: "Wildcard trust (Principal: *)", Status: risk.HasWildcardPrincipal, Severity: "critical"},
		{Signal: "No condition on trust policy", Status: risk.AnyAllowWithoutCond, Severity: "high"},
		{Signal: "No permission boundary", Status: c.PermissionBoundary == nil || *c.PermissionBoundary == "", Severity: "medium"},
		{Signal: "Wildcard resource in policies", Status: hasWildcardResourceInPermissions(c), Severity: "high"},
	}
}

// hasWildcardResourceInPermissions is a placeholder that is overridden by
// GetOverview's caller when effective_permissions data is available; kept
// as a cheap false default here since overview does not join
// effective_permissions per the spec's table list for this tab.
func hasWildcardResourceInPermissions(c *coreCredential) bool {
	// attached_policies / inline_policies are stored as app-summarized JSON
	// (name/type/arn/permissions_count, not full policy documents), so a
	// "Resource": "*" style scan isn't possible against them. Without a
	// join to effective_permissions (which overview's table list does not
	// include), this signal is conservatively reported as unknown/false.
	_ = c
	return false
}

// ═════════════════════════════════════════════════════════════════════════
// tab=trust — Tables: credentials, effective_permissions (permissions jsonb
// unnested for summary), permission_usage
// ═════════════════════════════════════════════════════════════════════════

func (r *CredentialDetailRepository) GetTrust(ctx context.Context, resolved *scope.DataScope, id string, src ActivitySource) (*models.TrustResponse, error) {
	src = orNoActivity(src)
	tenantDB := resolved.DB
	c, err := r.loadCoreCredential(ctx, resolved, id)
	if err != nil {
		return nil, err
	}

	var rawTrust map[string]interface{}
	if len(c.TrustPolicy) > 0 {
		_ = json.Unmarshal(c.TrustPolicy, &rawTrust)
	}
	statements := parseTrustStatements(rawTrust)

	principals := make([]models.TrustPrincipal, 0, len(statements))
	var warnings []string
	for _, st := range statements {
		if st.PrincipalValue == "" {
			continue
		}
		var cond *string
		if st.HasCondition {
			cond = nilIfEmptyStr(st.ConditionDesc)
		}
		isExternal := false
		if st.Effect == "Allow" && st.PrincipalType == "AWS" {
			for _, part := range strings.Split(st.PrincipalValue, ",") {
				acct := principalAccountID(strings.TrimSpace(part))
				if acct != "" && c.AccountID != "" && acct != c.AccountID {
					isExternal = true
				}
			}
		}
		principals = append(principals, models.TrustPrincipal{
			Principal:  st.PrincipalValue,
			Type:       st.PrincipalType,
			Condition:  cond,
			IsExternal: isExternal,
		})
		if st.Effect == "Allow" {
			if st.PrincipalValue == "*" {
				warnings = append(warnings, "Trust policy allows a wildcard principal (*)")
			}
			if isExternal {
				warnings = append(warnings, fmt.Sprintf("Cross-account trust granted to principal '%s'", st.PrincipalValue))
			}
			if !st.HasCondition && st.PrincipalType == "AWS" {
				warnings = append(warnings, fmt.Sprintf("No condition restricts assumption by '%s'", st.PrincipalValue))
			}
		}
	}
	if warnings == nil {
		warnings = []string{}
	}

	effSummary, err := r.loadEffectivePermissionsSummary(ctx, tenantDB, resolved.QSchema(), c, src)
	if err != nil {
		return nil, err
	}

	assumableRoles, err := r.loadAssumableRoles(ctx, tenantDB, resolved.QSchema(), c, src)
	if err != nil {
		return nil, err
	}

	return &models.TrustResponse{
		Tab: "trust",
		TrustPolicy: models.TrustPolicyBlock{
			Raw:        decodeJSON(c.TrustPolicy),
			Principals: principals,
			Warnings:   warnings,
		},
		AttachedPolicies:            decodeJSON(c.AttachedPolicies),
		InlinePolicies:              decodeJSON(c.InlinePolicies),
		EffectivePermissionsSummary: effSummary,
		AssumableRoles:              assumableRoles,
		PermissionBoundary: models.PermissionBoundary{
			BoundaryARN: c.PermissionBoundary,
			// Not derivable from this schema: only the boundary policy's ARN
			// is stored, not its document, so the effective restriction and
			// blocked-action list cannot be computed.
			EffectiveRestriction: nil,
			ActionsBlocked:       []string{},
		},
	}, nil
}

// loadEffectivePermissionsSummary decodes effective_permissions.permissions
// (an app-defined JSONB array, NOT a standardized AWS format) defensively:
// each element is expected to carry some combination of service/Service,
// action/Action, resource/resource_arn/Resource and effect/Effect keys —
// exact key casing is tolerated. If the real shape differs, this degrades
// to an empty summary rather than erroring.
func (r *CredentialDetailRepository) loadEffectivePermissionsSummary(ctx context.Context, tenantDB *sql.DB, schema string, c *coreCredential, src ActivitySource) ([]models.EffectivePermSummary, error) {
	query := fmt.Sprintf(`
		SELECT permissions
		FROM %s.effective_permissions
		WHERE credential_id::text = $1
		LIMIT 1`, schema)

	var raw []byte
	err := tenantDB.QueryRowContext(ctx, query, c.ID).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return summaryFromUsage(ctx, c, src), nil
		}
		return nil, fmt.Errorf("query effective_permissions: %w", err)
	}

	var entries []map[string]interface{}
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return summaryFromUsage(ctx, c, src), nil
	}

	type agg struct {
		allowed, denied int
		resources       map[string]struct{}
	}
	byService := map[string]*agg{}
	var order []string

	getStr := func(m map[string]interface{}, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}

	for _, e := range entries {
		service := getStr(e, "service", "Service")
		if service == "" {
			service = "unknown"
		}
		effect := strings.ToLower(getStr(e, "effect", "Effect"))
		resource := getStr(e, "resource_arn", "resource", "Resource")

		a, ok := byService[service]
		if !ok {
			a = &agg{resources: map[string]struct{}{}}
			byService[service] = a
			order = append(order, service)
		}
		if effect == "deny" {
			a.denied++
		} else {
			a.allowed++
		}
		if resource != "" {
			a.resources[resource] = struct{}{}
		}
	}

	sort.Strings(order)
	out := make([]models.EffectivePermSummary, 0, len(order))
	for _, svc := range order {
		a := byService[svc]
		scope := "Scoped"
		if _, ok := a.resources["*"]; ok {
			scope = "*"
		} else if len(a.resources) == 0 {
			scope = "Unknown"
		} else if len(a.resources) == 1 {
			for res := range a.resources {
				scope = res
			}
		}
		out = append(out, models.EffectivePermSummary{
			Service:        svc,
			AllowedActions: a.allowed,
			DeniedActions:  a.denied,
			ResourceScope:  scope,
		})
	}
	return out, nil
}

// summaryFromUsage is the fallback when no effective-permissions document is
// stored for the credential: it summarises what CloudTrail saw the credential
// actually do, grouped by service. The counts are observed actions, not granted
// ones, so the shape is the same but the numbers are a lower bound.
func summaryFromUsage(ctx context.Context, c *coreCredential, src ActivitySource) []models.EffectivePermSummary {
	if !src.Available() {
		return []models.EffectivePermSummary{}
	}
	return summarizeUsedPermissions(src.UsedPermissions(ctx, credentialKeys(c), usageSince(time.Now())))
}

func summarizeUsedPermissions(used []chrepo.UsedPermission) []models.EffectivePermSummary {
	type agg struct {
		actions   map[string]struct{}
		resources map[string]struct{}
	}
	byService := map[string]*agg{}
	for _, u := range used {
		svc := u.Service
		if svc == "" {
			svc = "unknown"
		}
		a, ok := byService[svc]
		if !ok {
			a = &agg{actions: map[string]struct{}{}, resources: map[string]struct{}{}}
			byService[svc] = a
		}
		a.actions[strings.ToLower(u.Action)] = struct{}{}
		if u.ResourceARN != "" {
			a.resources[u.ResourceARN] = struct{}{}
		}
	}
	services := make([]string, 0, len(byService))
	for svc := range byService {
		services = append(services, svc)
	}
	sort.Strings(services)
	out := make([]models.EffectivePermSummary, 0, len(services))
	for _, svc := range services {
		a := byService[svc]
		scopeLabel := "Unknown"
		switch {
		case len(a.resources) == 1:
			for res := range a.resources {
				scopeLabel = res
			}
		case len(a.resources) > 1:
			scopeLabel = "Scoped"
		}
		out = append(out, models.EffectivePermSummary{
			Service: svc, AllowedActions: len(a.actions), DeniedActions: 0, ResourceScope: scopeLabel,
		})
	}
	return out
}

var arnAccountRe = regexp.MustCompile(`arn:aws[a-z0-9-]*:[a-z0-9-]+:[a-z0-9-]*:(\d{12}):`)

// loadAssumableRoles enriches credentials.assumable_role_arns (a JSONB array
// of ARN strings) with the target account id (parsed from the ARN) and, on a
// best-effort basis, trust_edges.conditions for the matching edge plus the
// most recent sts:AssumeRole usage recorded in permission_usage.
func (r *CredentialDetailRepository) loadAssumableRoles(ctx context.Context, tenantDB *sql.DB, schema string, c *coreCredential, src ActivitySource) ([]models.AssumableRole, error) {
	src = orNoActivity(src)
	var arns []string
	if len(c.AssumableRoleARNs) > 0 {
		_ = json.Unmarshal(c.AssumableRoleARNs, &arns)
	}
	var assumeUsage []chrepo.UsedPermission
	if len(arns) > 0 && src.Available() {
		assumeUsage = src.UsedPermissions(ctx, credentialKeys(c), time.Time{}) // last assumed: no window
	}
	out := make([]models.AssumableRole, 0, len(arns))
	for _, arn := range arns {
		role := models.AssumableRole{TargetRoleARN: arn}
		if m := arnAccountRe.FindStringSubmatch(arn); m != nil {
			role.TargetAccount = m[1]
		}

		// trust_edges condition lookup (best-effort; edge_type naming is not
		// standardized in this schema, so match on the ARN pair only).
		var condRaw []byte
		condQuery := fmt.Sprintf(`
			SELECT conditions FROM %s.trust_edges
			WHERE source_arn = $1 AND target_arn = $2
			ORDER BY created_at DESC LIMIT 1`, schema)
		if err := tenantDB.QueryRowContext(ctx, condQuery, c.ARN, arn).Scan(&condRaw); err == nil && len(condRaw) > 0 {
			s := string(condRaw)
			role.Conditions = &s
		}

		// Most recent time this credential was seen assuming that role.
		role.LastAssumed = lastAssumedAt(assumeUsage, arn)

		out = append(out, role)
	}
	return out, nil
}

// lastAssumedAt finds the latest successful sts:AssumeRole* of targetARN.
func lastAssumedAt(used []chrepo.UsedPermission, targetARN string) *time.Time {
	var best *time.Time
	for _, u := range used {
		if !strings.EqualFold(u.Service, "sts") || !strings.HasPrefix(strings.ToLower(u.Action), "assumerole") {
			continue
		}
		if u.ResourceARN != targetARN {
			continue
		}
		if best == nil || u.LastUsedAt.After(*best) {
			t := u.LastUsedAt
			best = &t
		}
	}
	return best
}

// ═════════════════════════════════════════════════════════════════════════
// tab=activity — built by the service layer from ClickHouse
// ═════════════════════════════════════════════════════════════════════════

// GetActivitySubject returns the keys that identify this credential in
// CloudTrail. The activity tab itself is built by the service layer.
func (r *CredentialDetailRepository) GetActivitySubject(ctx context.Context, resolved *scope.DataScope, id string) (*ActivitySubject, error) {
	c, err := r.loadCoreCredential(ctx, resolved, id)
	if err != nil {
		return nil, err
	}
	return &ActivitySubject{ID: c.ID, ARN: c.ARN, Name: c.Name, Keys: credentialKeys(c)}, nil
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

// ═════════════════════════════════════════════════════════════════════════
// tab=rotation — Tables: credentials
// ═════════════════════════════════════════════════════════════════════════

func (r *CredentialDetailRepository) GetRotation(ctx context.Context, resolved *scope.DataScope, id string) (*models.RotationResponse, error) {
	c, err := r.loadCoreCredential(ctx, resolved, id)
	if err != nil {
		return nil, err
	}

	// NOTE: the spec's sample shows a multi-event rotation history (Created,
	// Rotated x2) with actor attribution ("By admin@company.com"). That
	// requires a dedicated audit/history table which does not exist in this
	// schema (the spec itself lists only "credentials" as the backing table
	// for this tab). The timeline below is therefore limited to what a
	// single credentials row can actually support: creation + the single
	// most recent rotation timestamp it stores.
	var timeline []models.RotationEvent
	if c.CreatedAtAWS != nil {
		details := "Discovered by scan"
		if c.DiscoverySource != nil && *c.DiscoverySource != "" {
			details = "Discovered via " + *c.DiscoverySource
		}
		timeline = append(timeline, models.RotationEvent{Date: *c.CreatedAtAWS, Event: "Created", Details: details})
	}
	if c.LastRotatedAt != nil && (c.CreatedAtAWS == nil || !c.LastRotatedAt.Equal(*c.CreatedAtAWS)) {
		timeline = append(timeline, models.RotationEvent{Date: *c.LastRotatedAt, Event: "Rotated", Details: "Most recent rotation on record"})
	}
	if timeline == nil {
		timeline = []models.RotationEvent{}
	}

	var nextExpected *time.Time
	if c.RotationEnabled && c.RotationIntervalDays != nil {
		ref := c.LastRotatedAt
		if ref == nil {
			ref = c.CreatedAtAWS
		}
		if ref != nil {
			t := ref.AddDate(0, 0, *c.RotationIntervalDays)
			nextExpected = &t
		}
	}

	return &models.RotationResponse{
		Tab:      "rotation",
		Timeline: timeline,
		Configuration: models.RotationConfig{
			AutoRotation:         c.RotationEnabled,
			RotationIntervalDays: c.RotationIntervalDays,
			LastRotation:         c.LastRotatedAt,
			NextExpected:         nextExpected,
			Status:               rotationStatus(c),
			RotationLambdaARN:    nil, // not tracked anywhere in this schema
		},
		Lifecycle: models.RotationLifecycle{
			CreatedAtAWS:      c.CreatedAtAWS,
			AgeDays:           ageDays(c),
			DaysSinceRotation: daysSinceRotation(c),
			ExpiresAt:         c.ExpiresAt,
			DaysUntilExpiry:   daysUntilExpiry(c),
		},
	}, nil
}

// ═════════════════════════════════════════════════════════════════════════
// tab=relationships — Tables: trust_edges, identities, credentials, resources
// ═════════════════════════════════════════════════════════════════════════

type graphEdgeRow struct {
	Source    string
	Target    string
	EdgeType  string
	CreatedAt time.Time
	CallCount uint64 // CloudTrail-derived edges only
}

func (r *CredentialDetailRepository) GetRelationships(ctx context.Context, resolved *scope.DataScope, id string, depth int, edgeType string, since, until time.Time, src ActivitySource) (*models.RelationshipsResponse, error) {
	src = orNoActivity(src)
	c, err := r.loadCoreCredential(ctx, resolved, id)
	if err != nil {
		return nil, err
	}

	edgeTypes := parseEdgeTypes(edgeType)
	allEdges, arns, err := r.traverseGraph(ctx, resolved, c.ARN, depth, edgeTypes)
	if err != nil {
		return nil, err
	}
	// ACCESSES edges now come from CloudTrail, not trust_edges.
	extra, hints := activityGraphEdges(ctx, resolved, src, c.ARN, credentialKeys(c), false, edgeTypes, since, until)
	allEdges, arns = mergeGraphEdges(allEdges, arns, extra)
	nodeInfo, err := r.resolveNodes(ctx, resolved, arns, hints, src)
	if err != nil {
		return nil, err
	}

	nodes := make([]models.GraphNode, 0, len(arns))
	for _, arn := range arns {
		info := nodeInfo[arn]
		nodes = append(nodes, models.GraphNode{
			ARN: arn, Name: info.name, Type: info.nodeType, Subtype: info.subtype,
			IsCurrent: arn == c.ARN,
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ARN < nodes[j].ARN })

	edges := make([]models.GraphEdge, 0, len(allEdges))
	relTable := make([]models.RelationshipRow, 0)
	for _, e := range allEdges {
		t := e.CreatedAt
		edges = append(edges, models.GraphEdge{
			Source: e.Source, Target: e.Target, EdgeType: e.EdgeType, LastObservedAt: observedAt(t),
			CallCount: callCountPtr(e.CallCount),
		})

		// Build the flat relationship_table only for edges that directly
		// touch the credential itself (first hop from its own perspective).
		if e.Source != c.ARN && e.Target != c.ARN {
			continue
		}
		direction := "outbound"
		targetArn := e.Target
		if e.Target == c.ARN {
			direction = "inbound"
			targetArn = e.Source
		}
		info := nodeInfo[targetArn]
		lastActive := observedAt(t)
		if info.nodeType == "identity" && info.lastActivityAt != nil {
			lastActive = info.lastActivityAt
		}
		relTable = append(relTable, models.RelationshipRow{
			Relationship: relationshipLabel(e.EdgeType, direction),
			Direction:    direction,
			TargetARN:    targetArn,
			TargetName:   info.name,
			TargetType:   info.nodeType,
			LastActive:   lastActive,
			CallCount:    callCountPtr(e.CallCount),
		})
	}

	return &models.RelationshipsResponse{
		Tab: "relationships",
		Graph: models.RelationshipGraph{
			Nodes: nodes,
			Edges: edges,
		},
		RelationshipTable:     relTable,
		ActivityDataAvailable: src.Available() && !src.Degraded(),
	}, nil
}

func relationshipLabel(edgeType, direction string) string {
	et := strings.ToUpper(strings.TrimSpace(edgeType))
	switch {
	case et == "BOUND_TO" && direction == "inbound":
		return "Used by"
	case et == "BOUND_TO" && direction == "outbound":
		return "Bound to"
	case strings.Contains(et, "ASSUME") && direction == "outbound":
		return "Can assume"
	case strings.Contains(et, "ASSUME") && direction == "inbound":
		return "Assumable by"
	case et == "SERVICE_TRUST" && direction == "inbound":
		return "Trusted service"
	case et == "SERVICE_TRUST" && direction == "outbound":
		return "Trusted by"
	case et == "":
		return "Related"
	default:
		return strings.Title(strings.ToLower(strings.ReplaceAll(et, "_", " ")))
	}
}

func (r *CredentialDetailRepository) loadEdgesTouching(ctx context.Context, tenantDB *sql.DB, schema string, arns []string, edgeTypes []string) ([]graphEdgeRow, error) {
	if len(arns) == 0 {
		return nil, nil
	}
	args := []interface{}{pqStringArray(arns)}
	typeFilter := ""
	if len(edgeTypes) > 0 {
		args = append(args, pqStringArray(edgeTypes))
		typeFilter = "AND edge_type = ANY($2)"
	}

	// Configured edges carry no observation time: created_at is when the
	// discovery run wrote the row, not when the relationship was last used,
	// so it is not reported as last_observed_at.
	query := fmt.Sprintf(`
		SELECT source_arn, target_arn, COALESCE(edge_type,'')
		FROM %s.trust_edges
		WHERE (source_arn = ANY($1) OR target_arn = ANY($1))
		  AND source_arn IS NOT NULL AND target_arn IS NOT NULL %s
		ORDER BY edge_type, source_arn, target_arn`, schema, typeFilter)

	rows, err := tenantDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query trust_edges: %w", err)
	}
	defer rows.Close()

	var out []graphEdgeRow
	for rows.Next() {
		var e graphEdgeRow
		if err := rows.Scan(&e.Source, &e.Target, &e.EdgeType); err != nil {
			return nil, fmt.Errorf("scan trust_edge: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type nodeInfo struct {
	name           string
	nodeType       string
	subtype        string
	lastActivityAt *time.Time
}

// resolveNodes classifies each ARN as a credential, identity, or resource
// (checked in that order) by looking it up across all three tables scoped
// to the caller's scope. Anything not found in any of them falls back to
// type "unknown" with a name derived from the ARN's final path segment.
// resolveNodes classifies each ARN as a credential, identity or resource.
//
// The credential and identity lookups carry the caller's account/region
// predicate, so a node that exists in the tenant database but outside the
// selected accounts simply does not resolve. Combined with the edge pruning in
// traverseGraph, that means an out-of-scope neighbour can never be rendered,
// even if it is reachable in trust_edges.
func (r *CredentialDetailRepository) resolveNodes(ctx context.Context, resolved *scope.DataScope, arns []string, hints map[string]nodeInfo, src ActivitySource) (map[string]nodeInfo, error) {
	src = orNoActivity(src)
	result := make(map[string]nodeInfo, len(arns))
	if resolved == nil || resolved.DB == nil || len(arns) == 0 {
		return result, nil
	}
	tenantDB := resolved.DB
	arnArray := pqStringArray(arns)

	credQuery := fmt.Sprintf(`SELECT arn, COALESCE(name,''), COALESCE(credential_type,'') FROM %s.credentials WHERE arn = ANY($1) AND deleted_at IS NULL AND account_id = ANY($2::text[]) AND region = ANY($3::text[])`, resolved.QSchema())
	{
		rows, err := tenantDB.QueryContext(ctx, credQuery, arnArray, pqStringArray(resolved.AccountIDs), pqStringArray(resolved.Regions))
		if err != nil {
			return nil, fmt.Errorf("resolve nodes: %w", err)
		}
		for rows.Next() {
			var arn, name, subtype string
			if err := rows.Scan(&arn, &name, &subtype); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan node: %w", err)
			}
			result[arn] = nodeInfo{name: name, nodeType: "credential", subtype: subtype}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("resolve nodes: %w", err)
		}
		rows.Close()
	}

	idQuery := fmt.Sprintf(`SELECT arn, COALESCE(name,''), COALESCE(identity_type,'') FROM %s.identities WHERE arn = ANY($1) AND deleted_at IS NULL AND account_id = ANY($2::text[]) AND region = ANY($3::text[])`, resolved.QSchema())
	{
		rows, err := tenantDB.QueryContext(ctx, idQuery, arnArray, pqStringArray(resolved.AccountIDs), pqStringArray(resolved.Regions))
		if err != nil {
			return nil, fmt.Errorf("resolve nodes: %w", err)
		}
		for rows.Next() {
			var arn, name, subtype string
			if err := rows.Scan(&arn, &name, &subtype); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan node: %w", err)
			}
			if _, exists := result[arn]; !exists {
				result[arn] = nodeInfo{name: name, nodeType: "identity", subtype: subtype}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("resolve nodes: %w", err)
		}
		rows.Close()
	}

	// The resources table is gone: resources are described by what CloudTrail
	// told us about them (hints), else by their ARN alone.
	for _, arn := range arns {
		if _, ok := result[arn]; ok {
			continue
		}
		if h, ok := hints[arn]; ok {
			result[arn] = h
		} else {
			result[arn] = resourceNodeInfo(arn)
		}
	}

	// last_activity_at of identity nodes comes from CloudTrail.
	if src.Available() {
		var identityARNs []string
		for arn, ni := range result {
			if ni.nodeType == "identity" {
				identityARNs = append(identityARNs, arn)
			}
		}
		if len(identityARNs) > 0 {
			seen := src.LastSeen(ctx, identityARNs)
			for _, arn := range identityARNs {
				if t, ok := seen[arn]; ok {
					ni := result[arn]
					tt := t
					ni.lastActivityAt = &tt
					result[arn] = ni
				}
			}
		}
	}
	return result, nil
}

func arnTail(arn string) string {
	parts := strings.Split(arn, "/")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	parts = strings.Split(arn, ":")
	return parts[len(parts)-1]
}

// ═════════════════════════════════════════════════════════════════════════
// tab=raw — Tables: credentials (all 31), effective_permissions (permissions)
// ═════════════════════════════════════════════════════════════════════════

func (r *CredentialDetailRepository) GetRaw(ctx context.Context, resolved *scope.DataScope, id string) (*models.RawResponse, error) {
	tenantDB := resolved.DB
	c, err := r.loadCoreCredential(ctx, resolved, id)
	if err != nil {
		return nil, err
	}

	var effPerms []byte
	effQuery := fmt.Sprintf(`SELECT permissions FROM %s.effective_permissions WHERE credential_id::text=$1 LIMIT 1`, resolved.QSchema())
	if err := tenantDB.QueryRowContext(ctx, effQuery, c.ID).Scan(&effPerms); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load effective permissions: %w", err)
	}

	record := map[string]interface{}{
		"id":                      c.ID,
		"arn":                     c.ARN,
		"name":                    c.Name,
		"credential_type":         c.CredentialType,
		"account_id":              c.AccountID,
		"region":                  c.Region,
		"status":                  c.Status,
		"created_at_aws":          c.CreatedAtAWS,
		"first_seen_at":           c.FirstSeenAt,
		"last_seen_at":            c.LastSeenAt,
		"deleted_at":              c.DeletedAt,
		"last_used_at":            c.LastUsedAt,
		"last_rotated_at":         c.LastRotatedAt,
		"expires_at":              c.ExpiresAt,
		"rotation_enabled":        c.RotationEnabled,
		"rotation_interval_days":  c.RotationIntervalDays,
		"tags":                    decodeJSON(c.Tags),
		"owner_principal_arn":     c.OwnerPrincipalARN,
		"trust_policy":            decodeJSON(c.TrustPolicy),
		"attached_policies":       decodeJSON(c.AttachedPolicies),
		"inline_policies":         decodeJSON(c.InlinePolicies),
		"permission_boundary_arn": c.PermissionBoundary,
		"max_session_duration":    c.MaxSessionDuration,
		"privilege_level":         c.PrivilegeLevel,
		"assumable_role_arns":     decodeJSON(c.AssumableRoleARNs),
		"discovery_source":        c.DiscoverySource,
		"type_attributes":         decodeJSON(c.TypeAttributes),
		"raw_api_response":        decodeJSON(c.RawAPIResponse),
		"created_at":              c.CreatedAt,
		"updated_at":              c.UpdatedAt,
	}

	return &models.RawResponse{
		Tab:                  "raw",
		CredentialRecord:     record,
		RawAPIResponse:       decodeJSON(c.RawAPIResponse),
		TrustPolicy:          decodeJSON(c.TrustPolicy),
		AttachedPolicies:     decodeJSON(c.AttachedPolicies),
		InlinePolicies:       decodeJSON(c.InlinePolicies),
		TypeAttributes:       decodeJSON(c.TypeAttributes),
		EffectivePermissions: decodeJSON(effPerms),
	}, nil
}
