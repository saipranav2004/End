package genome

import (
	"encoding/json"
	"strings"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
	"nhi-backend-api/internal/workloadtypes"
)

// ---------------------------------------------------------------------------
// Discovery row → monitoring identity (schema guide §4, contract §1.3/§1.4)
// ---------------------------------------------------------------------------
//
// ASSUMPTION NOTICE: the pipeline's app/nhi_types.py and
// app/identity_discovery.py were not available. The ten families named in
// the documents (ECS, LAMBDA, EC2, GLUE, STEP_FUNCTIONS, CODEBUILD, SAGEMAKER,
// SSM, IAM, IOT) are exact; the other family names and the workload-key
// formats are assumed. Everything lives below so reconciling with the real
// pipeline code is a change to this block only. source_type values must fit
// the VARCHAR(16) monitoring columns.

// resolveMonitoringType maps a discovery identity type to its monitoring
// subtype and source_type family. Types that are not monitored, and families
// switched off with NHI_GENOME_WORKLOAD_TYPES, are not monitoring identities.
func resolveMonitoringType(identityType string) (subtype, family string, ok bool) {
	return workloadtypes.Resolve(identityType)
}

// monitoredIdentity is the normalized shape of one discovery row.
type monitoredIdentity struct {
	Key            repo.MonitoringKey // (source_type, identity_name)
	Subtype        string
	DisplayName    string
	Status         string // raw identities.status (§10.2: lifecycle values NEED CONFIRMATION)
	AccountID      string
	Region         string
	ServiceName    string
	ClusterName    string
	RoleARN        string // first matching role ARN in type_attributes (§10.2 role_arn)
	RoleName       string
	RoleType       string
	TaskDefinition string
	InstanceID     string
	WorkloadKey    string
}

func (i monitoredIdentity) ref() models.DashboardIdentityRef {
	return models.DashboardIdentityRef{ID: i.Key.SourceType + ":" + i.Key.IdentityName, SourceType: i.Key.SourceType, IdentityName: i.Key.IdentityName, DisplayName: i.DisplayName}
}

func (i monitoredIdentity) workloadKey() (repo.MonitoringKey, bool) {
	if i.WorkloadKey == "" || i.WorkloadKey == i.Key.IdentityName {
		return repo.MonitoringKey{}, false
	}
	return repo.MonitoringKey{SourceType: i.Key.SourceType, IdentityName: i.WorkloadKey}, true
}

// allKeys returns the keys to try, in order: identity key, then workload key.
func (i monitoredIdentity) allKeys() []repo.MonitoringKey {
	keys := []repo.MonitoringKey{i.Key}
	if wk, ok := i.workloadKey(); ok {
		keys = append(keys, wk)
	}
	return keys
}

// normalizeDiscoveryRow converts a discovery row; ok is false when the row
// is not a monitoring identity.
func normalizeDiscoveryRow(row repo.DiscoveryIdentityRow) (monitoredIdentity, bool) {
	sub, fam, ok := resolveMonitoringType(row.IdentityType)
	if !ok {
		return monitoredIdentity{}, false
	}
	attrs := map[string]any{}
	if len(row.TypeAttributes) > 0 {
		_ = json.Unmarshal(row.TypeAttributes, &attrs)
	}
	canonical := canonicalMonitoringARN(sub, row.ARN)
	resource := arnResourceName(canonical)

	id := monitoredIdentity{Key: repo.MonitoringKey{SourceType: fam}, Subtype: sub, Status: strings.TrimSpace(row.Status),
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
		id.Key.IdentityName = strings.ToLower(fam) + ":" + id.AccountID + ":" + id.Region + ":" + sub + ":" + dashed(firstNonEmpty(row.Name, id.ServiceName))
	}
	id.DisplayName = firstNonEmpty(row.Name, resource, id.Key.IdentityName)
	id.WorkloadKey = workloadKeyFor(id, sub, resource)
	return id, true
}

// canonicalMonitoringARN removes execution/version qualifiers that would turn
// one stable resource into many identities (contract §1.3).
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
		return firstNonEmpty(attrString(attrs, "function_name", "functionName"), resource)
	case "step_function":
		return firstNonEmpty(attrString(attrs, "state_machine_name", "stateMachineName"), resource)
	case "glue_job", "glue_crawler":
		return firstNonEmpty(attrString(attrs, "job_name", "jobName", "crawler_name", "crawlerName"), resource)
	case "ecs_task":
		return firstNonEmpty(attrString(attrs, "group", "task_group"), lastPathSegment(attrString(attrs, "task_definition_family")))
	}
	return firstNonEmpty(attrString(attrs, "name", "resource_name"), resource)
}

// workloadKeyFor derives the shared workload key. Formats follow the
// contract's example ("ecs:<account>:<region>:<cluster>:<service>:…"); the
// identity key is always tried first, so a mismatch only disables the
// shared-profile fallback.
func workloadKeyFor(id monitoredIdentity, sub, resource string) string {
	acct, region := id.AccountID, id.Region
	switch sub {
	case "ecs_task":
		family := lastPathSegment(id.TaskDefinition)
		if i := strings.LastIndexByte(family, ':'); i > 0 {
			family = family[:i]
		}
		parts := []string{"ecs", acct, region, dashed(id.ClusterName), dashed(firstNonEmpty(id.ServiceName, family)), dashed(family)}
		if id.RoleName != "" {
			parts = append(parts, id.RoleName)
		}
		return strings.Join(parts, ":")
	case "lambda_function":
		return "lambda:" + acct + ":" + region + ":" + dashed(firstNonEmpty(id.ServiceName, resource))
	case "step_function":
		return "step_functions:" + acct + ":" + region + ":" + dashed(firstNonEmpty(id.ServiceName, resource))
	case "ec2_instance":
		return "ec2:" + acct + ":" + region + ":" + dashed(firstNonEmpty(id.RoleName, id.InstanceID, resource))
	case "glue_job":
		return "glue:" + acct + ":" + region + ":job:" + dashed(firstNonEmpty(id.ServiceName, resource))
	case "glue_crawler":
		return "glue:" + acct + ":" + region + ":crawler:" + dashed(firstNonEmpty(id.ServiceName, resource))
	}
	return strings.ToLower(id.Key.SourceType) + ":" + acct + ":" + region + ":" + sub + ":" + dashed(firstNonEmpty(id.ServiceName, resource, id.DisplayName))
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
func round1(v float64) float64    { return float64(int(v*10+0.5)) / 10 }

// optionalString returns nil for "" so optional attributes serialize as null.
func optionalString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
