package identitydrift

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

// Repository reads identity drift rows from the tenant schema. The v3 drift
// tables are pipeline-owned: this API only reads them and never creates or
// alters them.
type Repository struct{}

// NewRepository creates the read-only repository.
func NewRepository() *Repository { return &Repository{} }

// DriftRow is the latest identity_drift_scores row for the identity key or
// its shared workload key. Optional metrics stay nil when the column is null
// or not represented in the row.
type DriftRow struct {
	Key              common.MonitoringKey
	WindowEnd        time.Time
	Status           *string
	Drift            *float64
	RawDrift         *float64
	ModelConfidence  *float64
	VerdictTrust     *float64
	RetrainingStatus *string
	PSIMax           *float64
	PSIMaxDimension  *string
	Reasons          []string
}

// Latest returns the newest drift row, preferring the identity key to the
// workload key. A missing table or schema mismatch is classified by the
// shared repository error wrapper and is handled by the service as unavailable.
func (r *Repository) Latest(ctx context.Context, resolved *scope.DataScope, keys []common.MonitoringKey) (*DriftRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	types, names := common.KeyArrays(keys)
	q := `SELECT d.source_type, d.identity_name, d.window_end, to_jsonb(d)
	      FROM unnest($1::text[], $2::text[]) WITH ORDINALITY AS k(source_type, identity_name, preference)
	      JOIN ` + resolved.Table("identity_drift_scores") + ` d
	        ON d.source_type = k.source_type AND d.identity_name = k.identity_name
	      WHERE d.window_end <= now()
	      ORDER BY k.preference, d.window_end DESC
	      LIMIT 1`
	var (
		row DriftRow
		raw []byte
		err error
	)
	err = resolved.DB.QueryRowContext(ctx, q, types, names).Scan(
		&row.Key.SourceType, &row.Key.IdentityName, &row.WindowEnd, &raw,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "identity drift score", err)
	}
	row.WindowEnd = row.WindowEnd.UTC()
	values := make(map[string]any)
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, common.WrapMonitoringErr(ctx, "decode identity drift score", err)
	}
	row.Status = jsonString(values, "status")
	row.Drift = jsonFloat(values, "drift")
	row.RawDrift = jsonFloat(values, "raw_drift")
	row.ModelConfidence = jsonFloat(values, "model_confidence")
	row.VerdictTrust = jsonFloat(values, "verdict_trust")
	row.RetrainingStatus = jsonString(values, "retraining_status")
	row.PSIMax = jsonFloat(values, "psi_max")
	row.PSIMaxDimension = jsonString(values, "psi_max_dimension")
	row.Reasons = jsonStrings(values, "reasons")
	return &row, nil
}

// Dimensions returns the per-dimension evidence for the selected drift
// window. Only contract fields are projected from each row; raw table JSON is
// never returned to callers.
func (r *Repository) Dimensions(ctx context.Context, resolved *scope.DataScope, key common.MonitoringKey, windowEnd time.Time) ([]Dimension, error) {
	q := `SELECT to_jsonb(d)
	      FROM ` + resolved.Table("identity_dimension_drift") + ` d
	      WHERE d.source_type = $1 AND d.identity_name = $2 AND d.window_end = $3
	      ORDER BY d.dimension`
	rows, err := resolved.DB.QueryContext(ctx, q, key.SourceType, key.IdentityName, windowEnd)
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "identity dimension drift", err)
	}
	defer rows.Close()
	out := make([]Dimension, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, common.WrapMonitoringErr(ctx, "identity dimension drift", err)
		}
		values := make(map[string]any)
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, common.WrapMonitoringErr(ctx, "decode identity dimension drift", err)
		}
		name := jsonString(values, "dimension")
		if name == nil || strings.TrimSpace(*name) == "" {
			continue
		}
		out = append(out, Dimension{
			Dimension:    *name,
			PSI:          jsonFloat(values, "psi", "psi_value"),
			JSD:          jsonFloat(values, "jsd", "jsd_value"),
			GTest:        jsonFloat(values, "g_test", "g_test_statistic", "g_test_p_value"),
			Contribution: jsonFloat(values, "contribution"),
		})
	}
	return out, common.WrapMonitoringErr(ctx, "identity dimension drift", rows.Err())
}

func jsonString(values map[string]any, names ...string) *string {
	for _, name := range names {
		value, ok := values[name]
		if !ok || value == nil {
			continue
		}
		if s, ok := value.(string); ok {
			return &s
		}
	}
	return nil
}

func jsonFloat(values map[string]any, names ...string) *float64 {
	for _, name := range names {
		value, ok := values[name]
		if !ok || value == nil {
			continue
		}
		switch v := value.(type) {
		case float64:
			return &v
		case json.Number:
			f, err := v.Float64()
			if err == nil {
				return &f
			}
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil {
				return &f
			}
		}
	}
	return nil
}

func jsonStrings(values map[string]any, name string) []string {
	value, ok := values[name]
	if !ok || value == nil {
		return []string{}
	}
	if items, ok := value.([]any); ok {
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if s, ok := value.(string); ok {
		var items []string
		if json.Unmarshal([]byte(s), &items) == nil && items != nil {
			return items
		}
	}
	return []string{}
}
