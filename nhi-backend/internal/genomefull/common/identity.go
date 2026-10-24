package common

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lib/pq"

	"nhi-backend-api/internal/scope"
	"nhi-backend-api/internal/workloadtypes"
)

// ---------------------------------------------------------------------------
// Discovery row → monitoring identity
// ---------------------------------------------------------------------------
//
// ASSUMPTION NOTICE: the discovery pipeline's exact subtype catalog and
// identity_discovery logic were not available while this normalizer was
// written. The families named below are believed exact; the workload-key
// formats are best-effort. Everything lives in this block so reconciling
// with the real pipeline is a change to this file only. source_type values
// must fit the VARCHAR(16) monitoring columns.

// SupportedSourceTypes returns the compact source families NHI Genome shows
// (all of them, or the ones NHI_GENOME_WORKLOAD_TYPES lists), sorted.
func SupportedSourceTypes() []string {
	list, _ := workloadtypes.Enabled()
	return list
}

func resolveMonitoringType(identityType string) (subtype, family string, ok bool) {
	return workloadtypes.Resolve(identityType)
}

// DashboardIdentityRef is the small identity block embedded in several list
// rows: id = source_type + ':' + identity_name, display_name from the
// normalizer.
type DashboardIdentityRef struct {
	ID           string `json:"id"`
	SourceType   string `json:"source_type"`
	IdentityName string `json:"identity_name"`
	DisplayName  string `json:"display_name"`
}

// MonitoredIdentity is the normalized shape of one discovery row.
type MonitoredIdentity struct {
	Key            MonitoringKey // (source_type, identity_name)
	Subtype        string
	DisplayName    string
	Status         string // raw identities.status (lifecycle values NEED CONFIRMATION)
	AccountID      string
	Region         string
	ServiceName    string
	ClusterName    string
	RoleARN        string // first matching role ARN in type_attributes
	RoleName       string
	RoleType       string
	TaskDefinition string
	InstanceID     string
	WorkloadKey    string
}

// Ref renders the identity as its small dashboard reference.
func (i MonitoredIdentity) Ref() DashboardIdentityRef {
	return DashboardIdentityRef{ID: i.Key.SourceType + ":" + i.Key.IdentityName, SourceType: i.Key.SourceType, IdentityName: i.Key.IdentityName, DisplayName: i.DisplayName}
}

// PeerKey is the MVP peer key:
// source_type + account_id + region + service_name + cluster_name + role_type.
func (i MonitoredIdentity) PeerKey() string {
	return strings.Join([]string{i.Key.SourceType, i.AccountID, i.Region, i.ServiceName, i.ClusterName, i.RoleType}, "|")
}

// WorkloadMonitoringKey returns the shared workload key as a MonitoringKey,
// when it differs from the identity's own key.
func (i MonitoredIdentity) WorkloadMonitoringKey() (MonitoringKey, bool) {
	if i.WorkloadKey == "" || i.WorkloadKey == i.Key.IdentityName {
		return MonitoringKey{}, false
	}
	return MonitoringKey{SourceType: i.Key.SourceType, IdentityName: i.WorkloadKey}, true
}

// AllKeys returns the keys to try, in order: identity key, then workload key.
func (i MonitoredIdentity) AllKeys() []MonitoringKey {
	keys := []MonitoringKey{i.Key}
	if wk, ok := i.WorkloadMonitoringKey(); ok {
		keys = append(keys, wk)
	}
	return keys
}

// NormalizeDiscoveryRow converts a discovery row; ok is false when the row
// is not a monitoring identity.
func NormalizeDiscoveryRow(row DiscoveryIdentityRow) (MonitoredIdentity, bool) {
	sub, fam, ok := resolveMonitoringType(row.IdentityType)
	if !ok {
		return MonitoredIdentity{}, false
	}
	attrs := map[string]any{}
	if len(row.TypeAttributes) > 0 {
		_ = json.Unmarshal(row.TypeAttributes, &attrs)
	}
	canonical := canonicalMonitoringARN(sub, row.ARN)
	resource := arnResourceName(canonical)

	id := MonitoredIdentity{Key: MonitoringKey{SourceType: fam}, Subtype: sub, Status: strings.TrimSpace(row.Status),
		AccountID: strings.TrimSpace(row.AccountID), Region: strings.TrimSpace(row.Region)}
	if id.AccountID == "" {
		id.AccountID = arnField(canonical, 4)
	}
	if id.Region == "" {
		id.Region = arnField(canonical, 3)
	}
	// Runtime metadata from type_attributes (key names vary by scanner version).
	id.ClusterName = lastPathSegment(attrString(attrs, "cluster_name", "clusterName", "cluster", "cluster_arn", "clusterArn"))
	id.ServiceName = attrString(attrs, "service_name", "serviceName", "service")
	id.TaskDefinition = attrString(attrs, "task_definition", "taskDefinition", "task_definition_arn", "taskDefinitionArn", "task_definition_family")
	id.InstanceID = attrString(attrs, "instance_id", "instanceId")
	if roleARN := attrString(attrs, "bound_role_arn", "role_arn", "roleArn", "task_role_arn", "taskRoleArn",
		"execution_role_arn", "executionRoleArn", "iam_role_arn", "service_role_arn", "serviceRoleArn",
		"service_role", "execution_role", "role", "instance_profile_role_arn", "job_role_arn"); roleARN != "" {
		id.RoleARN = roleARN
		id.RoleName = arnResourceName(roleARN)
	}
	if sub == "ec2_instance" {
		id.RoleType = "EC2_INSTANCE_PROFILE"
		if id.InstanceID == "" {
			id.InstanceID = resource
		}
	} else {
		id.RoleType = attrString(attrs, "role_type", "roleType")
	}
	if id.ServiceName == "" {
		id.ServiceName = serverlessName(sub, attrs, resource)
	}
	// Stable identity key: canonical ARN, otherwise a typed fallback key. The
	// discovery row id is never used (it changes between scans).
	if canonical != "" {
		id.Key.IdentityName = canonical
	} else {
		id.Key.IdentityName = strings.ToLower(fam) + ":" + id.AccountID + ":" + id.Region + ":" + sub + ":" + dashed(FirstNonEmpty(row.Name, id.ServiceName))
	}
	id.DisplayName = FirstNonEmpty(row.Name, resource, id.Key.IdentityName)
	id.WorkloadKey = workloadKeyFor(id, sub, resource)
	return id, true
}

// canonicalMonitoringARN removes execution/version qualifiers that would turn
// one stable resource into many identities.
func canonicalMonitoringARN(sub, arn string) string {
	arn = strings.TrimSpace(arn)
	parts := arnParts(arn)
	if parts == nil {
		return arn
	}
	prefix, resource := strings.Join(parts[:5], ":"), parts[5]
	seg := strings.Split(resource, ":")
	switch sub {
	case "lambda_function": // function:name[:qualifier] → function:name
		if len(seg) >= 2 && seg[0] == "function" {
			return prefix + ":function:" + seg[1]
		}
	case "step_function": // execution:machine:id | stateMachine:name[:ver] → stateMachine:name
		if len(seg) >= 2 && (seg[0] == "execution" || seg[0] == "express" || seg[0] == "stateMachine") {
			return prefix + ":stateMachine:" + seg[1]
		}
	}
	return arn
}

func serverlessName(sub string, attrs map[string]any, resource string) string {
	switch sub {
	case "lambda_function":
		return FirstNonEmpty(attrString(attrs, "function_name", "functionName"), resource)
	case "step_function":
		return FirstNonEmpty(attrString(attrs, "state_machine_name", "stateMachineName"), resource)
	case "glue_job", "glue_crawler":
		return FirstNonEmpty(attrString(attrs, "job_name", "jobName", "crawler_name", "crawlerName"), resource)
	case "ecs_task":
		return FirstNonEmpty(attrString(attrs, "group", "task_group"), lastPathSegment(attrString(attrs, "task_definition_family")))
	}
	return FirstNonEmpty(attrString(attrs, "name", "resource_name"), resource)
}

// workloadKeyFor derives the shared workload key. The identity key is
// always tried first, so a mismatch only disables the shared-profile
// fallback.
func workloadKeyFor(id MonitoredIdentity, sub, resource string) string {
	acct, region := id.AccountID, id.Region
	switch sub {
	case "ecs_task":
		family := lastPathSegment(id.TaskDefinition)
		if i := strings.LastIndexByte(family, ':'); i > 0 {
			family = family[:i]
		}
		parts := []string{"ecs", acct, region, dashed(id.ClusterName), dashed(FirstNonEmpty(id.ServiceName, family)), dashed(family)}
		if id.RoleName != "" {
			parts = append(parts, id.RoleName)
		}
		return strings.Join(parts, ":")
	case "lambda_function":
		return "lambda:" + acct + ":" + region + ":" + dashed(FirstNonEmpty(id.ServiceName, resource))
	case "step_function":
		return "step_functions:" + acct + ":" + region + ":" + dashed(FirstNonEmpty(id.ServiceName, resource))
	case "ec2_instance":
		return "ec2:" + acct + ":" + region + ":" + dashed(FirstNonEmpty(id.RoleName, id.InstanceID, resource))
	case "glue_job":
		return "glue:" + acct + ":" + region + ":job:" + dashed(FirstNonEmpty(id.ServiceName, resource))
	case "glue_crawler":
		return "glue:" + acct + ":" + region + ":crawler:" + dashed(FirstNonEmpty(id.ServiceName, resource))
	}
	return strings.ToLower(id.Key.SourceType) + ":" + acct + ":" + region + ":" + sub + ":" + dashed(FirstNonEmpty(id.ServiceName, resource, id.DisplayName))
}

// ---- small helpers ----------------------------------------------------------

func attrString(attrs map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := attrs[k].(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		case float64, bool:
			b, _ := json.Marshal(v)
			return string(b)
		}
	}
	return ""
}

func arnParts(arn string) []string {
	if !strings.HasPrefix(arn, "arn:") {
		return nil
	}
	p := strings.SplitN(arn, ":", 6)
	if len(p) < 6 {
		return nil
	}
	return p
}

func arnField(arn string, i int) string {
	if p := arnParts(arn); p != nil {
		return p[i]
	}
	return ""
}

// arnResourceName returns the meaningful last part of an ARN:
// "function:name" → name, "task/cluster/id" → id, "role/path/name" → name.
func arnResourceName(arn string) string {
	p := arnParts(arn)
	if p == nil {
		return ""
	}
	res := p[5]
	if i := strings.LastIndexByte(res, ':'); i >= 0 && isAllDigits(res[i+1:]) {
		res = res[:i] // drop a trailing numeric revision such as ":42"
	}
	if i := strings.LastIndexByte(res, '/'); i >= 0 {
		return res[i+1:]
	}
	if i := strings.LastIndexByte(res, ':'); i >= 0 {
		return res[i+1:]
	}
	return res
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func lastPathSegment(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func dashed(s string) string {
	if s = strings.TrimSpace(s); s == "" {
		return "-"
	}
	return s
}

// FirstNonEmpty returns the first non-blank value.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func IntPtr(v int) *int           { return &v }
func FloatPtr(v float64) *float64 { return &v }
func Round1(v float64) float64    { return float64(int(v*10+0.5)) / 10 }

// OptionalString returns nil for "" so optional attributes serialize as null.
func OptionalString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------------------
// Discovery read (PostgreSQL identities table)
// ---------------------------------------------------------------------------

// DiscoveryIdentityRow is one latest discovery snapshot (raw, not normalized).
type DiscoveryIdentityRow struct {
	ID             string
	ARN            string
	Name           string
	IdentityType   string
	AccountID      string
	Region         string
	Status         string
	TypeAttributes []byte
	LastSeenAt     *time.Time
}

// LatestIdentities returns the latest non-deleted discovery snapshot per
// (identity_type, arn, account_id) inside the scope's accounts/regions.
// Unlike the inventory endpoints it is not pinned to one scan_id: the
// monitoring pipeline keys its tables by stable resource, not by scan.
func LatestIdentities(ctx context.Context, resolved *scope.DataScope) ([]DiscoveryIdentityRow, error) {
	q := `WITH ranked AS (
	SELECT id::text AS id, arn, name, identity_type, account_id, COALESCE(region, '') AS region, status, type_attributes, last_seen_at,
	       ROW_NUMBER() OVER (PARTITION BY identity_type, arn, account_id
	                          ORDER BY last_seen_at DESC NULLS LAST, updated_at DESC NULLS LAST, created_at DESC NULLS LAST, id DESC) AS row_rank
	FROM ` + resolved.Table("identities") + `
	WHERE deleted_at IS NULL
	  AND account_id = ANY($1::text[])
	  AND COALESCE(region, '') = ANY($2::text[])
)
SELECT id, arn, name, identity_type, account_id, region, status, type_attributes, last_seen_at
FROM ranked WHERE row_rank = 1`
	rows, err := resolved.DB.QueryContext(ctx, q, pq.Array(resolved.AccountIDs), pq.Array(resolved.Regions))
	if err != nil {
		return nil, WrapMonitoringErr(ctx, "latest identities", err)
	}
	defer rows.Close()
	var out []DiscoveryIdentityRow
	for rows.Next() {
		var row DiscoveryIdentityRow
		var arn, name, typ, account, status sql.NullString
		var attrs []byte
		var lastSeen sql.NullTime
		if err := rows.Scan(&row.ID, &arn, &name, &typ, &account, &row.Region, &status, &attrs, &lastSeen); err != nil {
			return nil, WrapMonitoringErr(ctx, "latest identities", err)
		}
		row.ARN, row.Name, row.IdentityType, row.AccountID, row.Status = arn.String, name.String, typ.String, account.String, status.String
		row.TypeAttributes = attrs
		if lastSeen.Valid {
			t := lastSeen.Time.UTC()
			row.LastSeenAt = &t
		}
		out = append(out, row)
	}
	return out, WrapMonitoringErr(ctx, "latest identities", rows.Err())
}

// ---------------------------------------------------------------------------
// Identity lookup: {source_type}/{identity_name} → one monitored identity
// ---------------------------------------------------------------------------

// ErrIdentityNotFound reports an unknown (source_type, identity_name) inside
// the caller's scope; handlers answer 404 IDENTITY_NOT_FOUND.
var ErrIdentityNotFound = fmt.Errorf("identity not found")

// ErrNotImplemented is returned for actions the current pipeline cannot
// perform (e.g. containment freeze); the handler maps it to 501 NOT_IMPLEMENTED.
var ErrNotImplemented = fmt.Errorf("containment actions are not available: the current pipeline has no containment audit log or worker")

// ResolvedIdentity is the lookup result: the tenant scope the identity was
// found in and its normalized form.
type ResolvedIdentity struct {
	Scope    *scope.DataScope
	Identity MonitoredIdentity
}

// MonitoredIdentityLookup resolves the {source_type}/{identity_name} path of
// the identity endpoints to one monitored identity inside the caller's
// tenant scope. It applies the same two steps as the dashboard: latest
// non-deleted discovery snapshot per resource from the tenant's identities
// table (account/region scoped), then the normalizer — so an identity is
// addressable only by the key the other endpoints emit, and never outside
// the caller's scope.
type MonitoredIdentityLookup struct {
	resolver ScopeResolver
}

// NewMonitoredIdentityLookup wires the lookup (pass the shared scope.Resolver).
func NewMonitoredIdentityLookup(resolver ScopeResolver) *MonitoredIdentityLookup {
	return &MonitoredIdentityLookup{resolver: resolver}
}

// Resolve finds the identity. Scope or discovery failures are
// *StoreUnavailableError (503); an unknown identity is ErrIdentityNotFound.
// When several snapshots normalize to the same key, the most recently seen
// one is used.
func (l *MonitoredIdentityLookup) Resolve(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string) (ResolvedIdentity, error) {
	sc, fleet, err := l.Fleet(ctx, userID, tenantID, dbName)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	for _, id := range fleet {
		if id.Key.SourceType == sourceType && id.Key.IdentityName == identityName {
			return ResolvedIdentity{Scope: sc, Identity: id}, nil
		}
	}
	return ResolvedIdentity{}, ErrIdentityNotFound
}

// Fleet returns the caller's tenant scope and every monitored identity in
// it: the latest non-deleted discovery snapshot per resource, normalized,
// one entry per (source_type, identity_name) — when several snapshots
// normalize to the same key the most recently seen one wins. Order follows
// the discovery read; callers sort. Failures are *StoreUnavailableError.
func (l *MonitoredIdentityLookup) Fleet(ctx context.Context, userID, tenantID, dbName string) (*scope.DataScope, []MonitoredIdentity, error) {
	sc, err := l.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, nil, &StoreUnavailableError{Op: "resolve tenant scope", Err: err}
	}
	rows, err := LatestIdentities(ctx, sc)
	if err != nil {
		return nil, nil, &StoreUnavailableError{Op: "read discovery identities", Err: err}
	}
	var (
		fleet []MonitoredIdentity
		index = map[MonitoringKey]int{}
		seen  = map[MonitoringKey]*DiscoveryIdentityRow{}
	)
	for i := range rows {
		id, ok := NormalizeDiscoveryRow(rows[i])
		if !ok {
			continue
		}
		if at, dup := index[id.Key]; dup {
			if newerSnapshot(rows[i], seen[id.Key]) {
				fleet[at], seen[id.Key] = id, &rows[i]
			}
			continue
		}
		index[id.Key] = len(fleet)
		seen[id.Key] = &rows[i]
		fleet = append(fleet, id)
	}
	return sc, fleet, nil
}

// newerSnapshot reports whether candidate was seen more recently than current.
func newerSnapshot(candidate DiscoveryIdentityRow, current *DiscoveryIdentityRow) bool {
	if current == nil || current.LastSeenAt == nil {
		return candidate.LastSeenAt != nil
	}
	return candidate.LastSeenAt != nil && candidate.LastSeenAt.After(*current.LastSeenAt)
}

// IdentityLinks are the relative paths of the identity's sub-resources.
type IdentityLinks struct {
	Activity string `json:"activity"`
	Genome   string `json:"genome"`
	Baseline string `json:"baseline"`
	Timeline string `json:"timeline"`
}

// IdentityLinksFor builds the links for one identity (identity_name is
// path-escaped, since it may be an ARN).
func IdentityLinksFor(sourceType, identityName string) IdentityLinks {
	base := "/api/v1/genome/identities/" + url.PathEscape(sourceType) + "/" + url.PathEscape(identityName)
	return IdentityLinks{Activity: base + "/activity", Genome: base + "/genome", Baseline: base + "/baseline", Timeline: base + "/timeline"}
}

// ---------------------------------------------------------------------------
// Identity index: lookups by identity key with workload-key fallback
// ---------------------------------------------------------------------------

// IdentityIndex indexes a fleet of monitored identities by their identity
// key and (as a fallback) their shared workload key.
type IdentityIndex struct {
	IDs        []MonitoredIdentity
	byKey      map[MonitoringKey]int
	byWorkload map[MonitoringKey]int // first identity sharing the workload key
}

// NewIdentityIndex builds the index over ids.
func NewIdentityIndex(ids []MonitoredIdentity) *IdentityIndex {
	set := &IdentityIndex{IDs: ids, byKey: map[MonitoringKey]int{}, byWorkload: map[MonitoringKey]int{}}
	for i, id := range ids {
		if _, dup := set.byKey[id.Key]; !dup {
			set.byKey[id.Key] = i
		}
		if wk, ok := id.WorkloadMonitoringKey(); ok {
			if _, dup := set.byWorkload[wk]; !dup {
				set.byWorkload[wk] = i
			}
		}
	}
	return set
}

// LookupKeys returns every key worth querying: identity keys plus workload
// keys (the pipeline may store a shared profile under the workload key).
func (s *IdentityIndex) LookupKeys() []MonitoringKey {
	out := make([]MonitoringKey, 0, len(s.byKey)+len(s.byWorkload))
	seen := map[MonitoringKey]struct{}{}
	for _, id := range s.IDs {
		for _, k := range id.AllKeys() {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				out = append(out, k)
			}
		}
	}
	return out
}

// IndexOf resolves a monitoring key (identity key first, then workload key).
func (s *IdentityIndex) IndexOf(k MonitoringKey) (int, bool) {
	if i, ok := s.byKey[k]; ok {
		return i, true
	}
	if i, ok := s.byWorkload[k]; ok {
		return i, true
	}
	return 0, false
}

// CompareDesc orders two optional values descending with nulls last; cmp
// compares the two present values ascending.
func CompareDesc(aPresent, bPresent bool, cmp func() int) int {
	switch {
	case aPresent && bPresent:
		return -cmp()
	case aPresent:
		return -1
	case bPresent:
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Event formatting shared by the identity activity/genome and anomaly
// endpoints
// ---------------------------------------------------------------------------

// APIOperation is the formula event_source + ':' + event_name, with the
// CloudTrail ".amazonaws.com" host suffix of event_source removed
// ("dynamodb:Query").
func APIOperation(eventSource, eventName string) string {
	return strings.TrimSuffix(strings.TrimSpace(eventSource), ".amazonaws.com") + ":" + strings.TrimSpace(eventName)
}

// SplitResourceARNs splits the comma-joined log_events.resource_arns,
// dropping blanks.
func SplitResourceARNs(joined string) []string {
	var out []string
	for _, part := range strings.Split(joined, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// FirstResourceARN is the first ARN the event touched ("" when the event has
// none).
func FirstResourceARN(joined string) string {
	if arns := SplitResourceARNs(joined); len(arns) > 0 {
		return arns[0]
	}
	return ""
}

// FingerprintDimensions are the dimensions_json keys the API may parse for
// the behavioural fingerprint, including the ECS- and Step
// Functions-specific ones; keys absent from an identity's events simply
// yield no points.
var FingerprintDimensions = []string{
	"api_operation", "api_service", "region", "read_write", "result", "resource_type", "resource_pattern",
	"source_network_class", "user_agent_family", "hour_of_week_utc", "timing",
	"cluster_name", "launch_type", "task_definition_family", // ECS
	"state_machine_name", "execution_status", // Step Functions
}
