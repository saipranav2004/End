package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/v1"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	"nhi-backend-api/internal/scope"
)

// ErrIdentityNotFound is returned when the requested :id has no matching,
// non-deleted row in identities. Handlers must check this with errors.Is
// (never string-match) and translate it to 404.
var ErrIdentityNotFound = errors.New("identity not found")

type IdentityDetailRepository struct{}

func NewIdentityDetailRepository() *IdentityDetailRepository {
	return &IdentityDetailRepository{}
}

// coreIdentity mirrors every column of the identities table (28 columns,
// confirmed via information_schema.columns against the live database —
// same provenance, and cross-checked against the legacy, pre-existing
// internal/repository/inventory/identities_detail.go query, as
// coreCredential in credential_detail_repo.go).
type coreIdentity struct {
	ID                   string
	ARN                  string
	Name                 string
	IdentityType         string
	FunctionalClass      *string
	AccountID            string
	Region               string
	Status               string
	CreatedAtAWS         *time.Time
	FirstSeenAt          *time.Time
	LastSeenAt           *time.Time
	LastActivityAt       *time.Time
	DeletedAt            *time.Time
	VPCID                *string
	SubnetID             *string
	SecurityGroupIDs     []byte
	IsPubliclyAccessible bool
	Tags                 []byte
	Owner                *string
	Environment          *string
	ParentARN            *string
	CloudtrailCreatorARN *string
	DiscoverySource      *string
	TypeAttributes       []byte
	RawAPIResponse       []byte
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
	ScopeAccountIDs      []string
	ScopeRegions         []string
}

// loadCoreIdentity fetches the row only if it belongs to the authenticated
// tenant's selected account/region scope.
func (r *IdentityDetailRepository) loadCoreIdentity(ctx context.Context, resolved *scope.DataScope, id string) (*coreIdentity, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, ErrIdentityNotFound
	}
	query := fmt.Sprintf(`
		SELECT
			id::text, arn, name, identity_type, functional_class,
			account_id, region, status,
			created_at_aws, first_seen_at, last_seen_at, last_activity_at, deleted_at,
			vpc_id, subnet_id, security_group_ids, COALESCE(is_publicly_accessible, false),
			tags, owner, environment, parent_arn, cloudtrail_creator_arn,
			discovery_source, type_attributes, raw_api_response,
			created_at, updated_at
		FROM %s.identities
		WHERE id = $1::uuid
		  AND account_id = ANY($2::text[]) AND region = ANY($3::text[])
		  AND deleted_at IS NULL
		LIMIT 1`, resolved.QSchema())

	var ci coreIdentity
	err := resolved.DB.QueryRowContext(ctx, query, id, pq.Array(resolved.AccountIDs), pq.Array(resolved.Regions)).Scan(
		&ci.ID, &ci.ARN, &ci.Name, &ci.IdentityType, &ci.FunctionalClass,
		&ci.AccountID, &ci.Region, &ci.Status,
		&ci.CreatedAtAWS, &ci.FirstSeenAt, &ci.LastSeenAt, &ci.LastActivityAt, &ci.DeletedAt,
		&ci.VPCID, &ci.SubnetID, &ci.SecurityGroupIDs, &ci.IsPubliclyAccessible,
		&ci.Tags, &ci.Owner, &ci.Environment, &ci.ParentARN, &ci.CloudtrailCreatorARN,
		&ci.DiscoverySource, &ci.TypeAttributes, &ci.RawAPIResponse,
		&ci.CreatedAt, &ci.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrIdentityNotFound
		}
		return nil, fmt.Errorf("load identity: %w", err)
	}
	ci.ScopeAccountIDs = append([]string(nil), resolved.AccountIDs...)
	ci.ScopeRegions = append([]string(nil), resolved.Regions...)
	return &ci, nil
}

// ── shared derivations ───────────────────────────────────────────────────

func identityAgeDays(ci *coreIdentity) *int {
	if ci.CreatedAtAWS == nil {
		return nil
	}
	d := daysBetween(*ci.CreatedAtAWS, time.Now())
	return &d
}

func identityIdleDays(ci *coreIdentity) *int {
	if ci.LastActivityAt == nil {
		return nil
	}
	d := daysBetween(*ci.LastActivityAt, time.Now())
	return &d
}

// deriveIdentityCategory is a best-effort, heuristic mapping from
// identity_type to a human-facing category. There is no stored "category"
// column in this schema, so this is a presentation-layer convenience only
// (same convention as deriveCategory for credentials in
// credential_detail_repo.go).
func deriveIdentityCategory(identityType string) string {
	switch strings.ToLower(strings.TrimSpace(identityType)) {
	case "ec2_instance", "ec2":
		return "Compute"
	case "lambda_function", "lambda":
		return "Serverless"
	case "ecs_task", "ecs_service", "fargate_task", "eks_pod", "eks_cluster", "container":
		return "Container"
	case "rds_instance", "rds_cluster", "dynamodb_table", "database":
		return "Database"
	case "s3_bucket":
		return "Storage"
	case "iam_role", "iam_user", "service_account":
		return "IAM"
	case "":
		return "Unknown"
	default:
		return strings.ToUpper(identityType[:1]) + identityType[1:]
	}
}

// deriveCredentialResolution is a best-effort, heuristic narrative of how
// this identity's credential was resolved, derived from identity_credentials
// .binding_type and (as a fallback) identities.discovery_source. There is
// no stored "credential_resolution" column in this schema.
func deriveCredentialResolution(bindingType string, discoverySource *string) string {
	switch strings.ToLower(strings.TrimSpace(bindingType)) {
	case "instance_profile":
		return "IAM instance profile association"
	case "execution_role":
		return "Execution role association"
	case "task_role":
		return "Task role association"
	case "service_linked_role", "service_role":
		return "Service-linked role association"
	case "assumed_role", "sts_assume_role":
		return "STS AssumeRole"
	}
	if discoverySource != nil && strings.TrimSpace(*discoverySource) != "" {
		return "Credential binding discovered via " + *discoverySource
	}
	return "Unknown binding mechanism"
}

// expectedTagKeys is a presentation-layer convention for "recommended"
// tags, not a stored configuration — there is no per-tenant required-tags
// table in this schema, so missing_tags is computed against this fixed
// list only.
var expectedTagKeys = []string{"Name", "Environment", "Owner", "CostCenter", "Project", "Team"}

func computeMissingTags(raw []byte) []string {
	missing := make([]string, 0)
	var tags map[string]interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &tags)
	}
	for _, k := range expectedTagKeys {
		if _, ok := tags[k]; !ok {
			missing = append(missing, k)
		}
	}
	return missing
}

func jsonArrayLen(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	var arr []interface{}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return 0
	}
	return len(arr)
}

// roundPctInt computes used/granted*100 rounded to the nearest integer, or
// nil when granted is zero/unknown (consistent with the +0.5 truncation
// idiom used by round1 elsewhere in this package).
func roundPctInt(used, granted int) *int {
	if granted <= 0 {
		return nil
	}
	v := int((float64(used)/float64(granted))*100 + 0.5)
	return &v
}

// boundCredentialKeys returns the distinct credential ids bound to this
// identity, plus the CloudTrail keys (ARN, access key ID) of
// each credential.
func (r *IdentityDetailRepository) boundCredentialKeys(ctx context.Context, tenantDB *sql.DB, schema string, ci *coreIdentity) ([]string, map[string][]string, error) {
	query := fmt.Sprintf(`
		SELECT DISTINCT c.id::text, COALESCE(c.arn,''), COALESCE(c.name,''), COALESCE(c.credential_type,'')
		FROM %s.identity_credentials ic
		JOIN %s.credentials c ON c.id::text = ic.credential_id::text
		WHERE ic.identity_id::text = $1 AND c.deleted_at IS NULL
		  AND c.account_id = ANY($2::text[]) AND c.region = ANY($3::text[])
		ORDER BY 1`, schema, schema)

	rows, err := tenantDB.QueryContext(ctx, query, ci.ID, pq.Array(ci.ScopeAccountIDs), pq.Array(ci.ScopeRegions))
	if err != nil {
		return nil, nil, fmt.Errorf("query bound credential ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	keys := map[string][]string{}
	for rows.Next() {
		var cid, arn, name, ctype string
		if err := rows.Scan(&cid, &arn, &name, &ctype); err != nil {
			return nil, nil, fmt.Errorf("scan credential id: %w", err)
		}
		ids = append(ids, cid)
		keys[cid] = credentialActivityKeys(arn, name, ctype)
	}
	return ids, keys, rows.Err()
}

// loadEffectivePermissions returns every permission entry recorded for one
// credential (all rows, in a stable order) and the number of actions they grant.
func loadEffectivePermissions(ctx context.Context, tenantDB *sql.DB, schema, credID string) ([]map[string]interface{}, int64, error) {
	q := fmt.Sprintf(`SELECT permissions, COALESCE(granted_count, 0) FROM %s.effective_permissions
		WHERE credential_id::text = $1 ORDER BY granted_count DESC NULLS LAST`, schema)
	rows, err := tenantDB.QueryContext(ctx, q, credID)
	if err != nil {
		return nil, 0, fmt.Errorf("query effective_permissions: %w", err)
	}
	defer rows.Close()
	var entries []map[string]interface{}
	var actions int64
	for rows.Next() {
		var raw []byte
		var n int64
		if err := rows.Scan(&raw, &n); err != nil {
			return nil, 0, fmt.Errorf("scan effective_permissions: %w", err)
		}
		var batch []map[string]interface{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &batch)
		}
		entries = append(entries, batch...)
		actions += n
	}
	return entries, actions, rows.Err()
}

// ActivitySubject identifies whose CloudTrail events to show on an activity tab.
type ActivitySubject struct {
	ID   string
	ARN  string
	Name string
	// Keys are the identity ARN, its bound credential ARNs and access key IDs
	// (for a credential: its ARN and key ID).
	Keys []string
}

// GetActivitySubject returns the keys that identify this identity in
// CloudTrail. The activity tab itself is built by the service layer from
// ClickHouse.
func (r *IdentityDetailRepository) GetActivitySubject(ctx context.Context, resolved *scope.DataScope, id string) (*ActivitySubject, error) {
	ci, err := r.loadCoreIdentity(ctx, resolved, id)
	if err != nil {
		return nil, err
	}
	ids, keysByCred, err := r.boundCredentialKeys(ctx, resolved.DB, resolved.QSchema(), ci)
	if err != nil {
		return nil, err
	}
	keys := []string{ci.ARN}
	for _, cid := range ids {
		keys = append(keys, keysByCred[cid]...)
	}
	return &ActivitySubject{ID: ci.ID, ARN: ci.ARN, Name: ci.Name, Keys: keys}, nil
}

// ═════════════════════════════════════════════════════════════════════════
// tab=overview — Tables: identities (all 28), identity_credentials,
// credentials, effective_permissions (granted_count), permission_usage
// ═════════════════════════════════════════════════════════════════════════

func (r *IdentityDetailRepository) GetOverview(ctx context.Context, resolved *scope.DataScope, id string, src ActivitySource) (*models.IdentityOverviewResponse, error) {
	src = orNoActivity(src)
	tenantDB := resolved.DB
	ci, err := r.loadCoreIdentity(ctx, resolved, id)
	if err != nil {
		return nil, err
	}

	boundCreds, credKeys, err := r.loadBoundCredentialsOverview(ctx, tenantDB, resolved.QSchema(), ci, src)
	if err != nil {
		return nil, err
	}

	// last_activity_at and created_by are no longer stored in Postgres; they
	// come from CloudTrail through the ActivitySource. When CloudTrail answered,
	// its answer (even "none") replaces the Postgres column.
	allKeys := []string{ci.ARN}
	for _, ks := range credKeys {
		allKeys = append(allKeys, ks...)
	}
	activityKnown := false
	if src.Available() {
		seen := src.LastSeen(ctx, allKeys)
		creator := src.Creators(ctx, []string{ci.ARN})[ci.ARN]
		if !src.Degraded() {
			activityKnown = true
			ci.LastActivityAt = latestOf(seen, allKeys)
			ci.CloudtrailCreatorARN = nilIfEmptyStr(creator)
		}
	}

	resolution := "Unknown binding mechanism"
	if len(boundCreds) > 0 {
		resolution = deriveCredentialResolution(boundCreds[0].BindingType, ci.DiscoverySource)
	} else if ci.DiscoverySource != nil {
		resolution = deriveCredentialResolution("", ci.DiscoverySource)
	}

	resp := &models.IdentityOverviewResponse{
		Tab: "overview",
		Identity: models.IdentityOverviewCore{
			ID: ci.ID, Name: ci.Name, ARN: ci.ARN, IdentityType: ci.IdentityType,
			FunctionalClass: ci.FunctionalClass, Category: deriveIdentityCategory(ci.IdentityType),
			AccountID: ci.AccountID, Region: ci.Region,
			VPCID: ci.VPCID, SubnetID: ci.SubnetID,
			SecurityGroupIDs:     decodeJSON(ci.SecurityGroupIDs),
			Environment:          ci.Environment,
			Owner:                ci.Owner,
			Status:               ci.Status,
			IsPubliclyAccessible: ci.IsPubliclyAccessible,
			CreatedAtAWS:         ci.CreatedAtAWS,
			AgeDays:              identityAgeDays(ci),
			FirstSeenAt:          ci.FirstSeenAt,
			LastSeenAt:           ci.LastSeenAt,
			LastActivityAt:       ci.LastActivityAt,
			IdleDays:             identityIdleDays(ci),
			ParentARN:            ci.ParentARN,
			CreatedBy:            ci.CloudtrailCreatorARN,
		},
		Discovery: models.IdentityDiscovery{
			DiscoverySource:      ci.DiscoverySource,
			CredentialResolution: resolution,
			DataSources:          "AWS API + CloudTrail correlation",
		},
		TypeAttributes:   decodeJSON(ci.TypeAttributes),
		BoundCredentials: boundCreds,
		Tags:             decodeJSON(ci.Tags),
		MissingTags:      computeMissingTags(ci.Tags),
	}

	resp.RiskSignals, err = r.computeIdentityRiskSignals(ctx, tenantDB, resolved.QSchema(), ci, boundCreds, activityKnown)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// loadBoundCredentialsOverview returns the identity's bound credentials and,
// in the same order, the CloudTrail keys (ARN, access key ID) of each.
func (r *IdentityDetailRepository) loadBoundCredentialsOverview(ctx context.Context, tenantDB *sql.DB, schema string, ci *coreIdentity, src ActivitySource) ([]models.IdentityBoundCredential, [][]string, error) {
	query := fmt.Sprintf(`
		SELECT c.id::text, COALESCE(c.name,''), COALESCE(c.credential_type,''), COALESCE(c.arn,''),
		       COALESCE(ic.binding_type,''), COALESCE(ic.is_primary,false),
		       c.trust_policy, c.attached_policies, c.inline_policies,
		       COALESCE(ep.granted_count,0)
		FROM %[1]s.identity_credentials ic
		JOIN %[1]s.credentials c ON c.id::text = ic.credential_id::text AND c.deleted_at IS NULL
		LEFT JOIN %[1]s.effective_permissions ep ON ep.credential_id::text = c.id::text
		WHERE ic.identity_id::text = $1
		  AND c.account_id = ANY($2::text[]) AND c.region = ANY($3::text[])
		ORDER BY ic.is_primary DESC, c.name NULLS LAST`, schema)

	rows, err := tenantDB.QueryContext(ctx, query, ci.ID, pq.Array(ci.ScopeAccountIDs), pq.Array(ci.ScopeRegions))
	if err != nil {
		return nil, nil, fmt.Errorf("query bound credentials: %w", err)
	}
	defer rows.Close()

	out := make([]models.IdentityBoundCredential, 0)
	var keys [][]string
	var grantedByIdx []int
	for rows.Next() {
		var bc models.IdentityBoundCredential
		var trustPolicy, attachedPolicies, inlinePolicies []byte
		var granted int
		var arn string
		if err := rows.Scan(&bc.CredentialID, &bc.Name, &bc.CredentialType, &arn, &bc.BindingType, &bc.IsPrimary,
			&trustPolicy, &attachedPolicies, &inlinePolicies, &granted); err != nil {
			return nil, nil, fmt.Errorf("scan bound credential: %w", err)
		}
		bc.GrantedActionsCount = granted
		bc.AttachedPoliciesCount = jsonArrayLen(attachedPolicies)
		bc.InlinePoliciesCount = jsonArrayLen(inlinePolicies)

		if len(trustPolicy) > 0 {
			var rawTrust map[string]interface{}
			if json.Unmarshal(trustPolicy, &rawTrust) == nil {
				for _, st := range parseTrustStatements(rawTrust) {
					if st.Effect == "Allow" && st.PrincipalValue != "" {
						v := st.PrincipalValue
						bc.TrustPrincipal = &v
						break
					}
				}
			}
		}

		out = append(out, bc)
		keys = append(keys, credentialActivityKeys(arn, bc.Name, bc.CredentialType))
		grantedByIdx = append(grantedByIdx, granted)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// used_actions_count: distinct actions CloudTrail saw this credential use.
	if src.Available() && len(out) > 0 {
		var flat []string
		for _, ks := range keys {
			flat = append(flat, ks...)
		}
		sets := src.UsedActionSets(ctx, flat, usageSince(time.Now()))
		for i := range out {
			used := distinctUsedCount(sets, keys[i])
			out[i].UsedActionsCount = used
			out[i].UtilizationPct = roundPctInt(used, grantedByIdx[i])
		}
	}
	return out, keys, nil
}

// computeIdentityRiskSignals builds the 8 risk signals called out explicitly
// by the spec's sample response, in the same order and with the same
// severities.
func (r *IdentityDetailRepository) computeIdentityRiskSignals(ctx context.Context, tenantDB *sql.DB, schema string, ci *coreIdentity, bound []models.IdentityBoundCredential, activityKnown bool) ([]models.RiskSignal, error) {
	// No recorded activity counts as stale, but only when CloudTrail could be
	// read: if it could not, "no activity" means "unknown", not "stale".
	stale := false
	if ci.LastActivityAt != nil {
		stale = daysBetween(*ci.LastActivityAt, time.Now()) > 90
	} else if activityKnown {
		stale = true
	}

	var neverRotated, expiringOrExpired, crossAccountTrust, wildcardResource bool

	for _, bc := range bound {
		var lastRotated, expiresAt sql.NullTime
		var trustPolicy, permsRaw []byte
		var credAccountID, credType string

		q := fmt.Sprintf(`SELECT last_rotated_at, expires_at, trust_policy, account_id, COALESCE(credential_type, '') FROM %s.credentials WHERE id::text = $1`, schema)
		if err := tenantDB.QueryRowContext(ctx, q, bc.CredentialID).Scan(&lastRotated, &expiresAt, &trustPolicy, &credAccountID, &credType); err != nil {
			continue // best-effort: a credential row that vanished mid-request shouldn't fail the whole response
		}

		if !lastRotated.Valid && rotatableCredential(credType) {
			neverRotated = true
		}
		if expiresAt.Valid {
			if expiresAt.Time.Before(time.Now()) {
				expiringOrExpired = true
			} else if daysBetween(time.Now(), expiresAt.Time) <= 30 {
				expiringOrExpired = true
			}
		}
		if len(trustPolicy) > 0 {
			var rawTrust map[string]interface{}
			if json.Unmarshal(trustPolicy, &rawTrust) == nil {
				risk := computeTrustRisk(parseTrustStatements(rawTrust), credAccountID)
				if risk.HasExternalAccount {
					crossAccountTrust = true
				}
			}
		}

		epQuery := fmt.Sprintf(`SELECT permissions FROM %s.effective_permissions WHERE credential_id::text = $1 LIMIT 1`, schema)
		if err := tenantDB.QueryRowContext(ctx, epQuery, bc.CredentialID).Scan(&permsRaw); err == nil && len(permsRaw) > 0 {
			var entries []map[string]interface{}
			if json.Unmarshal(permsRaw, &entries) == nil {
				for _, e := range entries {
					for _, key := range []string{"resource_arn", "resource", "Resource"} {
						if v, ok := e[key].(string); ok && v == "*" {
							wildcardResource = true
							break
						}
					}
					if wildcardResource {
						break
					}
				}
			}
		}
	}

	noOwner := ci.Owner == nil || strings.TrimSpace(*ci.Owner) == ""
	// Only workloads that run in a VPC can be outside one; an IAM user or a
	// function without VPC networking is not a finding.
	noVPC := vpcBoundIdentity(ci.IdentityType) && (ci.VPCID == nil || strings.TrimSpace(*ci.VPCID) == "")

	return []models.RiskSignal{
		{Signal: "Publicly accessible", Status: ci.IsPubliclyAccessible, Severity: "critical"},
		{Signal: "Stale (no activity > 90d)", Status: stale, Severity: "high"},
		{Signal: "No owner attribution", Status: noOwner, Severity: "medium"},
		{Signal: "Credential never rotated", Status: neverRotated, Severity: "high"},
		{Signal: "Credential expired/expiring", Status: expiringOrExpired, Severity: "critical"},
		{Signal: "Cross-account trust", Status: crossAccountTrust, Severity: "medium"},
		{Signal: "Wildcard resource scope", Status: wildcardResource, Severity: "high"},
		{Signal: "Not in a VPC", Status: noVPC, Severity: "medium"},
	}, nil
}

// ═════════════════════════════════════════════════════════════════════════
// tab=permissions — Tables: identity_credentials, effective_permissions
// (permissions JSONB — unnested here), permission_usage, credentials
// ═════════════════════════════════════════════════════════════════════════

func (r *IdentityDetailRepository) GetPermissions(ctx context.Context, resolved *scope.DataScope, id, service string, usedOnly *bool, src ActivitySource) (*models.IdentityPermissionsResponse, error) {
	src = orNoActivity(src)
	tenantDB := resolved.DB
	ci, err := r.loadCoreIdentity(ctx, resolved, id)
	if err != nil {
		return nil, err
	}

	resp := &models.IdentityPermissionsResponse{
		Tab:             "permissions",
		Permissions:     []models.IdentityPermissionItem{},
		PolicyBreakdown: []models.IdentityPolicyBreakdownItem{},
	}

	credIDs, keysByCred, err := r.boundCredentialKeys(ctx, tenantDB, resolved.QSchema(), ci)
	if err != nil {
		return nil, err
	}
	if len(credIDs) == 0 {
		return resp, nil
	}

	// What each credential actually used comes from CloudTrail. A granted
	// permission counts as used when a successful event is covered by it
	// (wildcards such as s3:Get* included).
	usedByCred := make(map[string][]chrepo.UsedPermission, len(credIDs))
	for _, credID := range credIDs {
		usedByCred[credID] = src.UsedPermissions(ctx, keysByCred[credID], usageSince(time.Now()))
	}

	type policyAgg struct {
		policyType  string
		total, used int
	}
	policyAggs := map[string]*policyAgg{}
	var policyOrder []string

	granted, usedCount := 0, 0

	getStr := func(m map[string]interface{}, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}

	var grantedActions int64
	for _, credID := range credIDs {
		entries, actions, err := loadEffectivePermissions(ctx, tenantDB, resolved.QSchema(), credID)
		if err != nil {
			return nil, err
		}
		grantedActions += actions

		for _, e := range entries {
			svc := getStr(e, "service", "Service")
			action := getStr(e, "action", "Action")
			if service != "" && !strings.EqualFold(svc, service) {
				continue
			}
			resourceArn := getStr(e, "resource_arn", "resource", "Resource")
			if resourceArn == "" {
				resourceArn = "*"
			}
			effect := getStr(e, "effect", "Effect")
			if effect == "" {
				effect = "Allow"
			}
			policySource := getStr(e, "policy_source", "policy_name", "PolicySource")
			if policySource == "" {
				policySource = "unknown"
			}
			policyType := getStr(e, "policy_type", "PolicyType")
			if policyType == "" {
				policyType = "managed"
			}

			used, lastUsedPtr, sourceIPs := usageFor(svc, action, usedByCred[credID])

			if usedOnly != nil && used != *usedOnly {
				continue
			}

			resp.Permissions = append(resp.Permissions, models.IdentityPermissionItem{
				Service: svc, Action: action, ResourceARN: resourceArn, Effect: effect,
				PolicySource: policySource, PolicyType: policyType, Used: used,
				LastUsedAt: lastUsedPtr, SourceIPs: sourceIPs,
			})

			granted++
			if used {
				usedCount++
			}

			pa, ok := policyAggs[policySource]
			if !ok {
				pa = &policyAgg{policyType: policyType}
				policyAggs[policySource] = pa
				policyOrder = append(policyOrder, policySource)
			}
			pa.total++
			if used {
				pa.used++
			}
		}
	}

	sort.Strings(policyOrder)
	for _, name := range policyOrder {
		pa := policyAggs[name]
		resp.PolicyBreakdown = append(resp.PolicyBreakdown, models.IdentityPolicyBreakdownItem{
			PolicyName: name, PolicyType: pa.policyType, TotalPermissions: pa.total, UsedPermissions: pa.used,
		})
	}

	// The summary uses the identity list's measure: distinct actions
	// CloudTrail saw used, out of the actions the policies grant
	// (effective_permissions.granted_count). Counting the policy entries
	// above instead would call one used "*:*" entry 100%. Without a granted
	// count the entries are all there is to count.
	if grantedActions > 0 && service == "" && usedOnly == nil {
		var keys []string
		for _, credID := range credIDs {
			keys = append(keys, keysByCred[credID]...)
		}
		granted = int(grantedActions)
		usedCount = distinctUsedCount(src.UsedActionSets(ctx, keys, usageSince(time.Now())), keys)
		if usedCount > granted {
			usedCount = granted
		}
	}
	unused := granted - usedCount
	utilPct := 0
	if p := roundPctInt(usedCount, granted); p != nil {
		utilPct = *p
	}
	resp.Summary = models.IdentityPermissionsSummary{
		GrantedCount: granted, UsedCount: usedCount, UnusedCount: unused, UtilizationPct: utilPct,
	}

	return resp, nil
}

// tab=activity is built by the service layer from ClickHouse (see
// GetActivitySubject); nothing in Postgres backs it any more.

// ═════════════════════════════════════════════════════════════════════════
// tab=relationships — Tables: trust_edges, identities, credentials, resources
//
// Reuses the BFS graph-walk helpers already implemented for Credential
// Detail's relationships tab (loadEdgesTouching / resolveNodes /
// relationshipLabel / graphEdgeRow / nodeInfo) by instantiating the
// (stateless, zero-field) CredentialDetailRepository — both types live in
// this same package. Only the entry point (this identity's own ARN
// instead of a credential's) differs.
// ═════════════════════════════════════════════════════════════════════════

func (r *IdentityDetailRepository) GetRelationships(ctx context.Context, resolved *scope.DataScope, id string, depth int, edgeType string, since, until time.Time, src ActivitySource) (*models.RelationshipsResponse, error) {
	src = orNoActivity(src)
	ci, err := r.loadCoreIdentity(ctx, resolved, id)
	if err != nil {
		return nil, err
	}

	cd := &CredentialDetailRepository{}

	edgeTypes := parseEdgeTypes(edgeType)
	allEdges, arns, err := cd.traverseGraph(ctx, resolved, ci.ARN, depth, edgeTypes)
	if err != nil {
		return nil, err
	}
	// ACCESSES and CREATED_BY edges now come from CloudTrail, not trust_edges.
	credIDs, keysByCred, err := r.boundCredentialKeys(ctx, resolved.DB, resolved.QSchema(), ci)
	if err != nil {
		return nil, err
	}
	keys := []string{ci.ARN}
	for _, cid := range credIDs {
		keys = append(keys, keysByCred[cid]...)
	}
	extra, hints := activityGraphEdges(ctx, resolved, src, ci.ARN, keys, true, edgeTypes, since, until)
	allEdges, arns = mergeGraphEdges(allEdges, arns, extra)
	nodeInfoMap, err := cd.resolveNodes(ctx, resolved, arns, hints, src)
	if err != nil {
		return nil, err
	}

	nodes := make([]models.GraphNode, 0, len(arns))
	for _, arn := range arns {
		info := nodeInfoMap[arn]
		nodes = append(nodes, models.GraphNode{ARN: arn, Name: info.name, Type: info.nodeType, Subtype: info.subtype, IsCurrent: arn == ci.ARN})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ARN < nodes[j].ARN })

	edges := make([]models.GraphEdge, 0, len(allEdges))
	relTable := make([]models.RelationshipRow, 0)
	for _, e := range allEdges {
		t := e.CreatedAt
		edges = append(edges, models.GraphEdge{Source: e.Source, Target: e.Target, EdgeType: e.EdgeType, LastObservedAt: observedAt(t), CallCount: callCountPtr(e.CallCount)})

		if e.Source != ci.ARN && e.Target != ci.ARN {
			continue
		}
		direction := "outbound"
		targetArn := e.Target
		if e.Target == ci.ARN {
			direction = "inbound"
			targetArn = e.Source
		}
		info := nodeInfoMap[targetArn]
		lastActive := observedAt(t)
		if info.nodeType == "identity" && info.lastActivityAt != nil {
			lastActive = info.lastActivityAt
		}
		relTable = append(relTable, models.RelationshipRow{
			Relationship: relationshipLabel(e.EdgeType, direction), Direction: direction,
			TargetARN: targetArn, TargetName: info.name, TargetType: info.nodeType, LastActive: lastActive,
			CallCount: callCountPtr(e.CallCount),
		})
	}

	return &models.RelationshipsResponse{
		Tab:                   "relationships",
		Graph:                 models.RelationshipGraph{Nodes: nodes, Edges: edges},
		RelationshipTable:     relTable,
		ActivityDataAvailable: src.Available() && !src.Degraded(),
	}, nil
}

// ═════════════════════════════════════════════════════════════════════════
// tab=credentials — Tables: identity_credentials, credentials (all 31),
// effective_permissions (granted_count, permissions), permission_usage
//
// Reuses loadCoreCredential / ageDays / daysSinceRotation / rotationOverdue
// / decodeJSON / nilIfEmptyStr / loadAssumableRoles from
// credential_detail_repo.go (same package) for every bound credential.
// ═════════════════════════════════════════════════════════════════════════

func (r *IdentityDetailRepository) GetCredentialsTab(ctx context.Context, resolved *scope.DataScope, id string, src ActivitySource) (*models.IdentityCredentialsTabResponse, error) {
	src = orNoActivity(src)
	tenantDB := resolved.DB
	ci, err := r.loadCoreIdentity(ctx, resolved, id)
	if err != nil {
		return nil, err
	}

	bindingQuery := fmt.Sprintf(`
		SELECT ic.credential_id::text, COALESCE(ic.binding_type,''), ic.binding_source, COALESCE(ic.is_primary,false)
		FROM %s.identity_credentials ic
		JOIN %s.credentials c ON c.id::text = ic.credential_id::text AND c.deleted_at IS NULL
		WHERE ic.identity_id::text = $1
		  AND c.account_id = ANY($2::text[]) AND c.region = ANY($3::text[])
		ORDER BY ic.is_primary DESC`, resolved.QSchema(), resolved.QSchema())

	rows, err := tenantDB.QueryContext(ctx, bindingQuery, ci.ID, pq.Array(ci.ScopeAccountIDs), pq.Array(ci.ScopeRegions))
	if err != nil {
		return nil, fmt.Errorf("query identity_credentials: %w", err)
	}

	type binding struct {
		credID        string
		bindingType   string
		bindingSource *string
		isPrimary     bool
	}
	var bindings []binding
	for rows.Next() {
		var b binding
		if err := rows.Scan(&b.credID, &b.bindingType, &b.bindingSource, &b.isPrimary); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan identity_credentials: %w", err)
		}
		bindings = append(bindings, b)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	cd := &CredentialDetailRepository{}
	resp := &models.IdentityCredentialsTabResponse{Tab: "credentials", Credentials: make([]models.IdentityCredentialDetailItem, 0, len(bindings))}

	for _, b := range bindings {
		c, err := cd.loadCoreCredential(ctx, resolved, b.credID)
		if err != nil {
			if errors.Is(err, ErrCredentialNotFound) {
				continue // bound credential row vanished (e.g. deleted since last discovery) — skip, don't fail the whole tab
			}
			return nil, err
		}

		applyCredentialLastUsed(ctx, c, src)

		var rawTrust map[string]interface{}
		if len(c.TrustPolicy) > 0 {
			_ = json.Unmarshal(c.TrustPolicy, &rawTrust)
		}
		statements := parseTrustStatements(rawTrust)
		principals := make([]models.TrustPrincipal, 0, len(statements))
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
				Principal: st.PrincipalValue, Type: st.PrincipalType, Condition: cond, IsExternal: isExternal,
			})
		}

		assumableRoles, err := cd.loadAssumableRoles(ctx, tenantDB, resolved.QSchema(), c, src)
		if err != nil {
			return nil, err
		}

		resp.Credentials = append(resp.Credentials, models.IdentityCredentialDetailItem{
			CredentialID: c.ID, Name: c.Name, ARN: c.ARN, CredentialType: c.CredentialType,
			BindingType: b.bindingType, BindingSource: b.bindingSource, IsPrimary: b.isPrimary,
			TrustPolicy: decodeJSON(c.TrustPolicy), TrustPrincipals: principals,
			AttachedPolicies: decodeJSON(c.AttachedPolicies), InlinePolicies: decodeJSON(c.InlinePolicies),
			Lifecycle: models.IdentityCredLifecycle{
				CreatedAtAWS: c.CreatedAtAWS, LastUsedAt: c.LastUsedAt, LastRotatedAt: c.LastRotatedAt,
				AgeDays: ageDays(c), DaysSinceRotation: daysSinceRotation(c),
				RotationEnabled: c.RotationEnabled, RotationIntervalDays: c.RotationIntervalDays,
				RotationOverdue: rotationOverdue(c), ExpiresAt: c.ExpiresAt,
				MaxSessionDuration: c.MaxSessionDuration, PermissionBoundaryARN: c.PermissionBoundary,
			},
			AssumableRoles: assumableRoles,
		})
	}

	return resp, nil
}

// ═════════════════════════════════════════════════════════════════════════
// tab=raw — Tables: identities (all 28), identity_credentials, credentials
// (raw_api_response, trust_policy, attached_policies, inline_policies,
// type_attributes), effective_permissions (permissions)
// ═════════════════════════════════════════════════════════════════════════

func (r *IdentityDetailRepository) GetRaw(ctx context.Context, resolved *scope.DataScope, id string) (*models.IdentityRawResponse, error) {
	tenantDB := resolved.DB
	ci, err := r.loadCoreIdentity(ctx, resolved, id)
	if err != nil {
		return nil, err
	}

	record := map[string]interface{}{
		"id": ci.ID, "arn": ci.ARN, "name": ci.Name, "identity_type": ci.IdentityType,
		"functional_class": ci.FunctionalClass, "account_id": ci.AccountID, "region": ci.Region,
		"status": ci.Status, "created_at_aws": ci.CreatedAtAWS, "first_seen_at": ci.FirstSeenAt,
		"last_seen_at": ci.LastSeenAt, "last_activity_at": ci.LastActivityAt, "deleted_at": ci.DeletedAt,
		"vpc_id": ci.VPCID, "subnet_id": ci.SubnetID, "security_group_ids": decodeJSON(ci.SecurityGroupIDs),
		"is_publicly_accessible": ci.IsPubliclyAccessible, "tags": decodeJSON(ci.Tags),
		"owner": ci.Owner, "environment": ci.Environment, "parent_arn": ci.ParentARN,
		"cloudtrail_creator_arn": ci.CloudtrailCreatorARN, "discovery_source": ci.DiscoverySource,
		"type_attributes":  decodeJSON(ci.TypeAttributes),
		"raw_api_response": decodeJSON(ci.RawAPIResponse), "created_at": ci.CreatedAt, "updated_at": ci.UpdatedAt,
	}

	// resolved_credential: derived from the identity's primary (or, lacking
	// one, first-bound) credential — there is no dedicated "resolved
	// credential" table, so this is assembled from identity_credentials +
	// credentials.
	var resolvedCred interface{}
	primaryQuery := fmt.Sprintf(`
		SELECT credential_id::text, COALESCE(binding_type,'')
		FROM %s.identity_credentials ic
		JOIN %s.credentials c ON c.id::text = ic.credential_id::text AND c.deleted_at IS NULL
		WHERE ic.identity_id::text = $1
		  AND c.account_id = ANY($2::text[]) AND c.region = ANY($3::text[])
		ORDER BY ic.is_primary DESC
		LIMIT 1`, resolved.QSchema(), resolved.QSchema())
	var primID, bindingType string
	if err := tenantDB.QueryRowContext(ctx, primaryQuery, ci.ID, pq.Array(ci.ScopeAccountIDs), pq.Array(ci.ScopeRegions)).Scan(&primID, &bindingType); err == nil {
		cd := &CredentialDetailRepository{}
		if c, err := cd.loadCoreCredential(ctx, resolved, primID); err == nil {
			resolvedCred = map[string]interface{}{
				"roleArn":          c.ARN,
				"assumedVia":       bindingType,
				"attachedPolicies": jsonArrayLen(c.AttachedPolicies),
				"inlinePolicies":   jsonArrayLen(c.InlinePolicies),
			}
		}
	}

	return &models.IdentityRawResponse{
		Tab: "raw", IdentityRecord: record, RawAPIResponse: decodeJSON(ci.RawAPIResponse),
		ResolvedCredential: resolvedCred, TypeAttributes: decodeJSON(ci.TypeAttributes),
	}, nil
}

// rotatableCredential reports whether a credential type is a long-lived
// secret someone has to rotate. IAM roles hand out short-lived session
// credentials, and an IAM user is rotated through its access keys, so
// neither is "never rotated".
func rotatableCredential(credentialType string) bool {
	switch strings.ToLower(strings.TrimSpace(credentialType)) {
	case "iam_role", "iam_user", "role", "user":
		return false
	}
	return true
}

// vpcBoundIdentity reports whether an identity type runs inside a VPC.
func vpcBoundIdentity(identityType string) bool {
	switch strings.ToLower(strings.TrimSpace(identityType)) {
	case "ec2_instance", "ecs_task", "eks_pod", "eks_node":
		return true
	}
	return false
}
