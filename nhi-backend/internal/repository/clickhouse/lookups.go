package clickhouse

import (
	"context"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// UsedPermission is one distinct (service, action, resource) that succeeded.
type UsedPermission struct {
	Service     string // short name, e.g. "s3"
	EventSource string
	Action      string // CloudTrail event name, e.g. "GetObject"
	ResourceARN string
	FirstUsedAt time.Time
	LastUsedAt  time.Time
	UsageCount  uint64
	SourceIPs   []string
	Regions     []string
}

// AccessEdge is a resource the subject successfully called.
type AccessEdge struct {
	TargetARN      string
	TargetType     string
	CallCount      uint64
	LastObservedAt time.Time
	Services       []string
}

const maxAccessEdges = 500

// maxUsedPermissions caps the (action, resource) pairs read per request.
// ponytail: the least-used pairs past the cap are dropped from "used"; raise
// it if a principal really calls this many distinct things in the window.
const maxUsedPermissions = 5000

// UsedPermissions is PDF function E: successful calls only.
func (c *Repository) UsedPermissions(ctx context.Context, sub Subject, sc Scope) ([]UsedPermission, error) {
	where, args, ok := buildFilter(sub, sc)
	if !ok {
		return nil, nil
	}
	q := fmt.Sprintf(`SELECT event_source, event_name, resource_arn,
			min(event_time), max(event_time), count(),
			groupUniqArray(source_ip), groupUniqArray(aws_region)
		FROM %s WHERE %s AND error_code = ''
		GROUP BY event_source, event_name, resource_arn
		ORDER BY count() DESC LIMIT %d`, c.table, where, maxUsedPermissions)

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse used permissions: %w", err)
	}
	defer rows.Close()

	var out []UsedPermission
	for rows.Next() {
		var p UsedPermission
		if err := rows.Scan(&p.EventSource, &p.Action, &p.ResourceARN, &p.FirstUsedAt, &p.LastUsedAt,
			&p.UsageCount, &p.SourceIPs, &p.Regions); err != nil {
			return nil, fmt.Errorf("clickhouse used permissions scan: %w", err)
		}
		p.Service = ServiceName(p.EventSource)
		out = append(out, p)
	}
	return out, rows.Err()
}

// LastSeenAt is PDF functions F and G: the most recent event involving the
// subject, or nil when there is none. (An empty aggregate in ClickHouse returns
// the epoch, not NULL, hence the count.)
func (c *Repository) LastSeenAt(ctx context.Context, sub Subject, sc Scope) (*time.Time, error) {
	where, args, ok := buildFilter(sub, sc)
	if !ok {
		return nil, nil
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var last time.Time
	var n uint64
	q := fmt.Sprintf("SELECT max(event_time), count() FROM %s WHERE %s", c.table, where)
	if err := c.conn.QueryRow(ctx, q, args...).Scan(&last, &n); err != nil {
		return nil, fmt.Errorf("clickhouse last seen: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	return &last, nil
}

// bulkChunk bounds how many keys go into one query; over HTTP the parameters
// travel in the URL.
const bulkChunk = 200

// LastSeenByKeys is the bulk form of F and G for list pages and the
// dashboard. Keys are identity ARNs, credential ARNs or access key IDs; an
// event counts toward a key if the key is its caller, its session issuer or
// its access key. Keys with no events are absent from the result.
func (c *Repository) LastSeenByKeys(ctx context.Context, keys []string, sc Scope) (map[string]time.Time, error) {
	out := make(map[string]time.Time)
	if len(keys) == 0 || len(sc.Accounts) == 0 {
		return out, nil
	}
	tc, targs := timeFilter(sc)
	q := fmt.Sprintf(`SELECT k, max(event_time) FROM (
			SELECT arrayJoin(arrayFilter(x -> x != '', [caller_arn, session_issuer_arn, access_key_id])) AS k, event_time
			FROM %s
			WHERE account_id IN {accounts:Array(String)}%s
			  AND (caller_arn IN {keys:Array(String)} OR session_issuer_arn IN {keys:Array(String)}
			       OR access_key_id IN {keys:Array(String)})
		) WHERE k IN {keys:Array(String)} GROUP BY k`, c.table, tc)

	for start := 0; start < len(keys); start += bulkChunk {
		end := min(start+bulkChunk, len(keys))
		args := append([]any{ch.Named("accounts", sc.Accounts), ch.Named("keys", keys[start:end])}, targs...)
		if err := c.collectLastSeen(ctx, q, args, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (c *Repository) collectLastSeen(ctx context.Context, q string, args []any, out map[string]time.Time) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("clickhouse bulk last seen: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var t time.Time
		if err := rows.Scan(&k, &t); err != nil {
			return fmt.Errorf("clickhouse bulk last seen scan: %w", err)
		}
		out[k] = t
	}
	return rows.Err()
}

// creationEvents are the events that mark a principal as the creator of a
// resource. The first group is the PDF's list; the IAM events were added
// because the PDF list cannot find who created a role or user.
var creationEvents = []string{
	"RunInstances", "CreateFunction", "RunTask", "CreateService",
	"CreateCluster", "CreateNodegroup", "CreateJob", "CreateCrawler",
	"RunJobFlow", "CreateNotebookInstance", "CreateTrainingJob",
	"CreateStateMachine", "CreateProject", "CreatePipeline",
	"PutRule", "CreateStack", "CreateRestApi", "CreateApi",
	"CreateDistribution", "CreateStream", "CreateDeliveryStream",
	"CreateDBInstance", "CreateDBCluster", "CreateQueue",
	"CreateTopic", "CreateVpcEndpoint", "CreateLoadBalancer",
	"CreateCacheCluster", "CreateDomain", "CreateBroker",
	"CreateDocument", "CreateServer", "CreateJobDefinition",
	"CreateEnvironment", "CreateInstances", "CreateDetector",
	"CreateRepository",
	"CreateRole", "CreateUser", "CreateInstanceProfile", "CreateServiceLinkedRole",
}

// CreatorARN is PDF function H: who created the resource, or "" if unknown.
// Only a successful creation counts: a denied attempt created nothing.
func (c *Repository) CreatorARN(ctx context.Context, identityARN string, accounts []string) (string, error) {
	if identityARN == "" || len(accounts) == 0 {
		return "", nil
	}
	q := fmt.Sprintf(`SELECT caller_arn FROM %s
		WHERE resource_arn = {arn:String} AND event_name IN {events:Array(String)}
		  AND caller_arn != '' AND error_code = '' AND account_id IN {accounts:Array(String)}
		ORDER BY event_time ASC LIMIT 1`, c.table)

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q,
		ch.Named("arn", identityARN), ch.Named("events", creationEvents), ch.Named("accounts", accounts))
	if err != nil {
		return "", fmt.Errorf("clickhouse creator: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return "", rows.Err()
	}
	var creator string
	if err := rows.Scan(&creator); err != nil {
		return "", fmt.Errorf("clickhouse creator scan: %w", err)
	}
	return creator, nil
}

// AccessEdges is PDF function I: resources the subject successfully called.
// Failed calls (for example an AccessDenied AssumeRole) are not edges.
func (c *Repository) AccessEdges(ctx context.Context, sub Subject, sc Scope) ([]AccessEdge, error) {
	where, args, ok := buildFilter(sub, sc)
	if !ok {
		return nil, nil
	}
	q := fmt.Sprintf(`SELECT resource_arn, resource_type, count() AS call_count,
			max(event_time), groupUniqArray(event_source)
		FROM %s WHERE %s AND resource_arn != '' AND error_code = ''
		GROUP BY resource_arn, resource_type
		ORDER BY call_count DESC LIMIT %d`, c.table, where, maxAccessEdges)

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse access edges: %w", err)
	}
	defer rows.Close()

	var out []AccessEdge
	for rows.Next() {
		var e AccessEdge
		if err := rows.Scan(&e.TargetARN, &e.TargetType, &e.CallCount, &e.LastObservedAt, &e.Services); err != nil {
			return nil, fmt.Errorf("clickhouse access edges scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreatorsByARNs is the bulk form of CreatorARN: for each resource ARN, the
// caller of its earliest creation event. ARNs with no creation event are
// absent from the result.
func (c *Repository) CreatorsByARNs(ctx context.Context, arns []string, accounts []string) (map[string]string, error) {
	out := make(map[string]string)
	arns = cleanKeys(arns)
	if len(arns) == 0 || len(accounts) == 0 {
		return out, nil
	}
	q := fmt.Sprintf(`SELECT resource_arn, argMin(caller_arn, event_time) FROM %s
		WHERE resource_arn IN {arns:Array(String)} AND event_name IN {events:Array(String)}
		  AND caller_arn != '' AND error_code = '' AND account_id IN {accounts:Array(String)}
		GROUP BY resource_arn`, c.table)

	for start := 0; start < len(arns); start += bulkChunk {
		end := min(start+bulkChunk, len(arns))
		if err := func() error {
			ctx, cancel := c.withTimeout(ctx)
			defer cancel()
			rows, err := c.conn.Query(ctx, q,
				ch.Named("arns", arns[start:end]), ch.Named("events", creationEvents), ch.Named("accounts", accounts))
			if err != nil {
				return fmt.Errorf("clickhouse creators: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var arn, creator string
				if err := rows.Scan(&arn, &creator); err != nil {
					return fmt.Errorf("clickhouse creators scan: %w", err)
				}
				out[arn] = creator
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UsedAction is one distinct successful action: the service (short name) and
// the CloudTrail event name.
type UsedAction struct {
	Service string
	Action  string
}

// UsedActionsByKeys returns, per key (identity ARN, credential ARN or access
// key ID), the distinct successful actions seen since the given time (zero
// means no lower bound). It is the bulk input for permission-utilisation
// figures.
func (c *Repository) UsedActionsByKeys(ctx context.Context, keys []string, accounts []string, since time.Time) (map[string][]UsedAction, error) {
	out := make(map[string][]UsedAction)
	keys = cleanKeys(keys)
	if len(keys) == 0 || len(accounts) == 0 {
		return out, nil
	}
	timeClause, timeArgs := timeFilter(Scope{Start: since})
	q := fmt.Sprintf(`SELECT k, event_source, event_name FROM (
			SELECT arrayJoin(arrayFilter(x -> x != '', [caller_arn, session_issuer_arn, access_key_id])) AS k,
			       event_source, event_name
			FROM %s
			WHERE account_id IN {accounts:Array(String)} AND error_code = ''%s
			  AND (caller_arn IN {keys:Array(String)} OR session_issuer_arn IN {keys:Array(String)}
			       OR access_key_id IN {keys:Array(String)})
		) WHERE k IN {keys:Array(String)} GROUP BY k, event_source, event_name`, c.table, timeClause)

	for start := 0; start < len(keys); start += bulkChunk {
		end := min(start+bulkChunk, len(keys))
		if err := func() error {
			ctx, cancel := c.withTimeout(ctx)
			defer cancel()
			args := append([]any{ch.Named("accounts", accounts), ch.Named("keys", keys[start:end])}, timeArgs...)
			rows, err := c.conn.Query(ctx, q, args...)
			if err != nil {
				return fmt.Errorf("clickhouse used actions: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var k, source, name string
				if err := rows.Scan(&k, &source, &name); err != nil {
					return fmt.Errorf("clickhouse used actions scan: %w", err)
				}
				out[k] = append(out[k], UsedAction{Service: ServiceName(source), Action: name})
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ChangeEvidence is the CloudTrail call that created or deleted a resource.
type ChangeEvidence struct {
	CallerARN string
	EventName string
	SourceIP  string
	EventTime time.Time
}

// DeletionEvents are the calls that remove a principal or credential.
var DeletionEvents = []string{
	"DeleteRole", "DeleteUser", "DeleteAccessKey", "DeleteInstanceProfile",
	"DeleteServiceLinkedRole", "DeleteLoginProfile", "DeleteFunction",
	"TerminateInstances", "DeleteSecret", "ScheduleKeyDeletion",
	"DeleteCluster", "DeleteService", "DeleteStack", "DeleteServiceSpecificCredential",
}

// CredentialCreationEvents extend creationEvents with calls that issue
// credentials rather than workloads.
var CredentialCreationEvents = append(append([]string(nil), creationEvents...),
	"CreateAccessKey", "CreateServiceSpecificCredential", "CreateSecret", "CreateLoginProfile")

// ChangeEvidenceByARNs finds, for each ARN, the latest successful call of one
// of events against it in [from, to], within the tenant's accounts. ARNs with
// no such call are absent from the result.
func (c *Repository) ChangeEvidenceByARNs(ctx context.Context, arns, events, accounts []string, from, to time.Time) (map[string]ChangeEvidence, error) {
	out := map[string]ChangeEvidence{}
	arns = cleanKeys(arns)
	if len(arns) == 0 || len(events) == 0 || len(accounts) == 0 {
		return out, nil
	}
	q := fmt.Sprintf(`SELECT resource_arn, argMax(caller_arn, event_time), argMax(event_name, event_time),
		       argMax(source_ip, event_time), max(event_time)
		FROM %s
		WHERE resource_arn IN {arns:Array(String)} AND event_name IN {events:Array(String)}
		  AND account_id IN {accounts:Array(String)} AND error_code = ''
		  AND event_time >= {from:DateTime64(3)} AND event_time <= {to:DateTime64(3)}
		GROUP BY resource_arn`, c.table)
	for start := 0; start < len(arns); start += bulkChunk {
		end := min(start+bulkChunk, len(arns))
		if err := func() error {
			ctx, cancel := c.withTimeout(ctx)
			defer cancel()
			rows, err := c.conn.Query(ctx, q, ch.Named("arns", arns[start:end]), ch.Named("events", events),
				ch.Named("accounts", accounts), ch.Named("from", from.UTC()), ch.Named("to", to.UTC()))
			if err != nil {
				return fmt.Errorf("clickhouse change evidence: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var arn string
				var ev ChangeEvidence
				if err := rows.Scan(&arn, &ev.CallerARN, &ev.EventName, &ev.SourceIP, &ev.EventTime); err != nil {
					return fmt.Errorf("clickhouse change evidence scan: %w", err)
				}
				ev.EventTime = ev.EventTime.UTC()
				out[arn] = ev
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
