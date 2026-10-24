package identitybaseline

import (
	"context"
	"encoding/json"
	"sort"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/genomefull/identitydrift"
)

// Service implements
// GET /api/v1/genome/identities/{source_type}/{identity_name}/baseline —
// the baseline provenance panel:
//
//   - state fields     baselines (status, required/built/remaining days, dates)  (PostgreSQL)
//   - data_points      SUM(daily_stats.event_count) over the baseline window     (PostgreSQL)
//   - dimensions       safe summaries of baselines.profile_json                  (PostgreSQL)
//   - versions         baseline_versions with safe selected profile fields       (PostgreSQL)
//   - model confidence identity_drift_scores.model_confidence when available
//
// The baseline row is looked up by the identity key first and the shared
// workload key second (the fallback); data points and versions are then
// read under the key the baseline is actually stored under. Nothing is
// invented: without a baselines row every field is null and the lists are
// empty — the legitimate "still learning" state.
type Service struct {
	lookup    *common.MonitoredIdentityLookup
	repo      *Repository
	driftRepo *identitydrift.Repository
}

// NewService wires the service.
func NewService(lookup *common.MonitoredIdentityLookup, repo *Repository, driftRepo *identitydrift.Repository) *Service {
	return &Service{lookup: lookup, repo: repo, driftRepo: driftRepo}
}

// Params are the parsed query parameters.
type Params struct {
	VersionsLimit int // limit: rows of versions (newest first)
}

// GetBaseline builds the response. Unknown identities are
// common.ErrIdentityNotFound; scope/discovery failures are
// *common.StoreUnavailableError.
func (s *Service) GetBaseline(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string, p Params) (*Response, error) {
	res, err := s.lookup.Resolve(ctx, userID, tenantID, dbName, sourceType, identityName)
	if err != nil {
		return nil, err
	}
	sc := res.Scope
	keys := res.Identity.AllKeys() // identity key, then the workload key when it differs

	out := &Response{
		ModelConfidencePercent:   nil,
		ModelConfidenceAvailable: false,
		Dimensions:               []DimensionSummary{},
		Versions:                 []Version{},
	}
	if s.driftRepo != nil {
		if drift, err := s.driftRepo.Latest(ctx, sc, keys); common.ReadOK("identity baseline model confidence", err) && drift != nil && drift.ModelConfidence != nil {
			out.ModelConfidencePercent = drift.ModelConfidence
			out.ModelConfidenceAvailable = true
		}
	}

	row, err := s.repo.Baseline(ctx, sc, keys)
	if !common.ReadOK("identity baseline", err) || row == nil {
		if err := common.BudgetExceeded(ctx, "identity baseline"); err != nil {
			return nil, err
		}
		return out, nil
	}
	out.Status = common.OptionalString(row.Status)
	out.RequiredDays = common.IntPtr(row.RequiredDays)
	out.BuiltDays = common.IntPtr(row.BuiltDays)
	out.DaysRemaining = common.IntPtr(row.DaysRemaining)
	if row.FirstSeenDate != nil {
		d := row.FirstSeenDate.Format("2006-01-02")
		out.FirstSeenDate = &d
	}
	out.ActiveSince = row.ActiveSince
	updated := row.UpdatedAt
	out.UpdatedAt = &updated
	out.Dimensions = profileDimensions(row.Profile)

	if n, err := s.repo.DataPoints(ctx, sc, row.Key, row.RequiredDays); common.ReadOK("identity baseline data points", err) {
		out.DataPoints = &n
	}
	if versions, err := s.repo.Versions(ctx, sc, []common.MonitoringKey{row.Key}, p.VersionsLimit); common.ReadOK("identity baseline versions", err) {
		for _, v := range versions {
			out.Versions = append(out.Versions, Version{
				Version:     v.Version,
				PublishedAt: v.PublishedAt,
				Profile:     VersionProfile{Meta: profileMeta(v.Profile), Dimensions: profileDimensions(v.Profile)},
			})
		}
	}
	if err := common.BudgetExceeded(ctx, "identity baseline"); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- safe profile summaries ---------------------------------------------------
//
// profile_json holds "dimension models, frequency model, and _meta
// version/training metadata" (schema guide §7); its exact layout is not
// documented and may contain internal counters, which the endpoint must
// not return. Only the following is extracted:
//
//   - dimension models: the JSON members (top level, or one level below a
//     wrapper object) named like a monitoring dimension — the list the
//     fingerprint endpoint uses, i.e. the names alerts.dimension carries —
//     summarized as name, `ready` flag and, when the model is a plain
//     value → count map, the number of distinct values;
//   - `_meta`: its scalar members only (version, timestamps, configuration).

// profileDimensions returns the safe dimension summaries of a profile, in
// the fingerprint dimension order. Empty for an empty/unparseable profile.
func profileDimensions(raw []byte) []DimensionSummary {
	out := []DimensionSummary{}
	root, ok := parseProfile(raw)
	if !ok {
		return out
	}
	wanted := make(map[string]int, len(common.FingerprintDimensions))
	for i, d := range common.FingerprintDimensions {
		wanted[d] = i
	}
	found := map[string]any{}
	for name, v := range root {
		if name == "_meta" {
			continue
		}
		if _, ok := wanted[name]; ok {
			found[name] = v
			continue
		}
		if wrapper, ok := v.(map[string]any); ok { // e.g. {"dimensions": {"api_operation": {...}}}
			for inner, iv := range wrapper {
				if _, ok := wanted[inner]; ok {
					if _, dup := found[inner]; !dup {
						found[inner] = iv
					}
				}
			}
		}
	}
	for name, v := range found {
		out = append(out, summarizeDimensionModel(name, v))
	}
	sort.Slice(out, func(i, j int) bool { return wanted[out[i].Dimension] < wanted[out[j].Dimension] })
	return out
}

// summarizeDimensionModel reduces one dimension model to its safe summary.
func summarizeDimensionModel(name string, v any) DimensionSummary {
	d := DimensionSummary{Dimension: name}
	switch m := v.(type) {
	case map[string]any:
		if ready, ok := m["ready"].(bool); ok {
			d.Ready = &ready
		}
		if n, ok := valueCountOf(m); ok {
			d.ValueCount = &n
		}
	case []any:
		n := len(m)
		d.ValueCount = &n
	}
	return d
}

// valueCountOf reports the size of m when it is a plain value → count map
// (every member numeric); other layouts are not interpreted.
func valueCountOf(m map[string]any) (int, bool) {
	if len(m) == 0 {
		return 0, false
	}
	for _, v := range m {
		if _, isNumber := v.(float64); !isNumber {
			return 0, false
		}
	}
	return len(m), true
}

// profileMeta returns the scalar members of the profile's `_meta` object
// (nil when there is none).
func profileMeta(raw []byte) map[string]any {
	root, ok := parseProfile(raw)
	if !ok {
		return nil
	}
	meta, ok := root["_meta"].(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for k, v := range meta {
		switch v.(type) {
		case string, float64, bool, nil:
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseProfile(raw []byte) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil || len(root) == 0 {
		return nil, false
	}
	return root, true
}
