package genome

import (
	"context"
	"encoding/json"
	"sort"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
)

// IdentityBaselineService implements
// GET /api/v1/identities/{source_type}/{identity_name}/baseline
// (backend_api_contract.md §5.5 / §10.3) — the baseline provenance panel:
//
//   - state fields     baselines (status, required/built/remaining days, dates)  (PostgreSQL)
//   - data_points      SUM(daily_stats.event_count) over the baseline window     (PostgreSQL)
//   - dimensions       safe summaries of baselines.profile_json                 (PostgreSQL)
//   - versions         baseline_versions with safe selected profile fields       (PostgreSQL)
//   - model confidence not stored anywhere → null + available:false (§10.3)
//
// The baseline row is looked up by the identity key first and the shared
// workload key second (the contract's fallback); data points and versions
// are then read under the key the baseline is actually stored under.
// Nothing is invented: without a baselines row every field is null and the
// lists are empty — the legitimate "still learning" state.
type IdentityBaselineService struct {
	lookup *MonitoredIdentityLookup
	repo   *repo.IdentityBaselineRepository
}

// NewIdentityBaselineService wires the service.
func NewIdentityBaselineService(lookup *MonitoredIdentityLookup, repo *repo.IdentityBaselineRepository) *IdentityBaselineService {
	return &IdentityBaselineService{lookup: lookup, repo: repo}
}

// IdentityBaselineParams are the parsed query parameters.
type IdentityBaselineParams struct {
	VersionsLimit int // §2 limit: rows of versions (newest first)
}

// GetBaseline builds the §5.5 response. Unknown identities are
// ErrIdentityNotFound; scope/discovery failures are *StoreUnavailableError.
func (s *IdentityBaselineService) GetBaseline(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string, p IdentityBaselineParams) (*models.IdentityBaselineResponse, error) {
	res, err := s.lookup.Resolve(ctx, userID, tenantID, dbName, sourceType, identityName)
	if err != nil {
		return nil, err
	}
	sc := res.scope
	keys := res.identity.allKeys() // identity key, then the workload key when it differs

	out := &models.IdentityBaselineResponse{
		ModelConfidencePercent:   nil,
		ModelConfidenceAvailable: false,
		Dimensions:               []models.BaselineDimensionSummary{},
		Versions:                 []models.BaselineVersion{},
	}

	row, err := s.repo.Baseline(ctx, sc, keys)
	if !readOK("identity baseline", err) || row == nil {
		return out, nil
	}
	out.Status = optionalString(row.Status)
	out.RequiredDays = intPtr(row.RequiredDays)
	out.BuiltDays = intPtr(row.BuiltDays)
	out.DaysRemaining = intPtr(row.DaysRemaining)
	if row.FirstSeenDate != nil {
		d := row.FirstSeenDate.Format("2006-01-02")
		out.FirstSeenDate = &d
	}
	out.ActiveSince = row.ActiveSince
	updated := row.UpdatedAt
	out.UpdatedAt = &updated
	out.Dimensions = profileDimensions(row.Profile)

	if n, err := s.repo.DataPoints(ctx, sc, row.Key, row.RequiredDays); readOK("identity baseline data points", err) {
		out.DataPoints = &n
	}
	if versions, err := s.repo.Versions(ctx, sc, []repo.MonitoringKey{row.Key}, p.VersionsLimit); readOK("identity baseline versions", err) {
		for _, v := range versions {
			out.Versions = append(out.Versions, models.BaselineVersion{
				Version:     v.Version,
				PublishedAt: v.PublishedAt,
				Profile:     models.BaselineVersionProfile{Meta: profileMeta(v.Profile), Dimensions: profileDimensions(v.Profile)},
			})
		}
	}
	return out, nil
}

// ---- safe profile summaries ---------------------------------------------------
//
// profile_json holds "dimension models, frequency model, and _meta
// version/training metadata" (schema guide §7); its exact layout is not
// documented and may contain internal counters, which §5.5 forbids returning.
// Only the following is extracted:
//
//   - dimension models: the JSON members (top level, or one level below a
//     wrapper object) named like a monitoring dimension — the §5.4 list the
//     fingerprint uses, i.e. the names alerts.dimension carries — summarized
//     as name, `ready` flag and, when the model is a plain value → count map,
//     the number of distinct values;
//   - `_meta`: its scalar members only (version, timestamps, configuration).

// profileDimensions returns the safe dimension summaries of a profile, in
// the contract's dimension order. Empty for an empty/unparseable profile.
func profileDimensions(raw []byte) []models.BaselineDimensionSummary {
	out := []models.BaselineDimensionSummary{}
	root, ok := parseProfile(raw)
	if !ok {
		return out
	}
	wanted := make(map[string]int, len(genomeFingerprintDimensions))
	for i, d := range genomeFingerprintDimensions {
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
func summarizeDimensionModel(name string, v any) models.BaselineDimensionSummary {
	d := models.BaselineDimensionSummary{Dimension: name}
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
