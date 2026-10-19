package postures

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityPostureRepository struct{}

func NewIdentityPostureRepository() *IdentityPostureRepository {
	return &IdentityPostureRepository{}
}

// unusedAfterDays: a credential not used for longer than this is "Unused".
const unusedAfterDays = 90

// checkRow is one evaluated check of the identity.
type checkRow struct {
	RuleID     string
	Status     string
	Severity   string
	Pillar     string
	PointsLost float64
	Penalty    float64
	Fix        string
	Title      string
}

// Get implements GET /identities/{id}/posture: score card, pillars, "why this
// score", score impact, peer group and bound credentials.
func (r *IdentityPostureRepository) Get(ctx context.Context, s *scope.DataScope, id string, w models.Window) (*models.IdentityPosture, error) {
	if err := need(s); err != nil {
		return nil, err
	}
	out := &models.IdentityPosture{
		Change:           models.Change{Window: w.Label},
		Pillars:          []models.PillarScore{},
		ScoreImpact:      []models.ScoreImpactItem{},
		BoundCredentials: []models.BoundCredential{},
	}

	q := fmt.Sprintf(`SELECT COALESCE(identity_name, ''), COALESCE(identity_arn, ''), functional_class, score,
			COALESCE(band, ''), COALESCE(grade, ''), COALESCE(points_lost, 0), COALESCE(checks_failed, 0),
			COALESCE(checks_evaluated, 0), pillar_scores, why_this_score, COALESCE(open_alerts, 0), %s
		FROM %s WHERE tenant_id = $1 AND identity_id = $2`, w.ChangeColumn, s.Table(tIdentityPosture))
	var name, arn string
	var class, why sql.NullString
	var score, lost, change sql.NullFloat64
	var failed, evaluated, alerts sql.NullInt64
	var pillarJSON []byte
	err := s.DB.QueryRowContext(ctx, q, s.TenantID, id).Scan(&name, &arn, &class, &score, &out.Band, &out.Grade,
		&lost, &failed, &evaluated, &pillarJSON, &why, &alerts, &change)
	if errors.Is(err, sql.ErrNoRows) || isUndefinedTable(err) { // not scored yet
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load identity posture: %w", err)
	}
	out.Score = nullFloat(score)
	out.PointsLost = lost.Float64
	out.ChecksFailed = intVal(failed)
	out.ChecksEvaluated = intVal(evaluated)
	out.OpenAlerts = intVal(alerts)
	out.Change.Value = nullFloat(change)
	if score.Valid {
		out.AfterEveryFix = models.AfterEveryFix{Score: 100, Delta: round1(100 - score.Float64)}
	}

	checks, err := r.loadChecks(ctx, s, id)
	if err != nil {
		return nil, err
	}
	if out.ChecksEvaluated == 0 {
		out.ChecksEvaluated = len(checks)
	}
	out.ScoreImpact = scoreImpact(checks)
	if out.Pillars, err = r.pillars(ctx, s, pillarJSON, checks); err != nil {
		return nil, err
	}
	out.WhyThisScore = strings.TrimSpace(why.String)
	if out.WhyThisScore == "" {
		out.WhyThisScore = describeScore(name, score, checks)
	}

	if class.Valid && strings.TrimSpace(class.String) != "" {
		if out.PeerGroup, err = r.peerGroup(ctx, s, id, class.String, score); err != nil {
			return nil, err
		}
	}
	if arn != "" {
		if out.BoundCredentials, err = r.boundCredentials(ctx, s, arn); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *IdentityPostureRepository) loadChecks(ctx context.Context, s *scope.DataScope, id string) ([]checkRow, error) {
	q := fmt.Sprintf(`SELECT c.rule_id, COALESCE(c.status, ''), COALESCE(c.severity, ''), COALESCE(c.pillar_code, ''),
			COALESCE(c.points_lost, 0), COALESCE(c.penalty_points, 0),
			COALESCE(NULLIF(w.fix_label, ''), NULLIF(w.check_title, ''), NULLIF(w.rule_name, ''), c.rule_id),
			COALESCE(NULLIF(w.check_title, ''), NULLIF(w.rule_name, ''), c.rule_id)
		FROM %s c LEFT JOIN %s w ON w.rule_id = c.rule_id
		WHERE c.tenant_id = $1 AND c.identity_id = $2`, s.Table(tIdentityChecks), s.Table(tRuleWeights))
	rows, err := s.DB.QueryContext(ctx, q, s.TenantID, id)
	if err != nil {
		return nil, fmt.Errorf("load identity checks: %w", err)
	}
	defer rows.Close()
	var out []checkRow
	for rows.Next() {
		var c checkRow
		if err := rows.Scan(&c.RuleID, &c.Status, &c.Severity, &c.Pillar, &c.PointsLost, &c.Penalty, &c.Fix, &c.Title); err != nil {
			return nil, fmt.Errorf("scan identity check: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// scoreImpact lists the failing checks that cost points, biggest first.
func scoreImpact(checks []checkRow) []models.ScoreImpactItem {
	out := make([]models.ScoreImpactItem, 0)
	for _, c := range checks {
		if strings.EqualFold(c.Status, "FAIL") && c.PointsLost > 0 {
			out = append(out, models.ScoreImpactItem{RuleID: c.RuleID, Fix: c.Fix, Points: c.PointsLost})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Points != out[j].Points {
			return out[i].Points > out[j].Points
		}
		return out[i].RuleID < out[j].RuleID
	})
	return out
}

// describeScore builds the sentence used when the worker stored none.
func describeScore(name string, score sql.NullFloat64, checks []checkRow) string {
	impact := scoreImpact(checks)
	if !score.Valid || len(impact) == 0 {
		return ""
	}
	titles := map[string]string{}
	for _, c := range checks {
		titles[c.RuleID] = c.Title
	}
	parts := make([]string, 0, 3)
	for i, it := range impact {
		if i == 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s fails (%g points)", titles[it.RuleID], it.Points))
	}
	return fmt.Sprintf("%s scores %g because %s.", name, round1(score.Float64), strings.Join(parts, ", "))
}

// identityPillarStatus: FAIL = a Critical or High check fails · WARN = only
// Medium or Low checks fail · PASS = nothing fails. Accepted or suppressed
// findings cost 0 points and so do not count as failing.
func identityPillarStatus(failing []checkRow) string {
	status := "PASS"
	for _, c := range failing {
		switch strings.ToLower(c.Severity) {
		case "critical", "high":
			return "FAIL"
		default:
			status = "WARN"
		}
	}
	return status
}

// pillars merges the stored pillar scores with the catalog and the checks.
// Numbers missing from the stored JSON are derived from the checks:
// 100 × (1 − points lost ÷ applicable penalties).
func (r *IdentityPostureRepository) pillars(ctx context.Context, s *scope.DataScope, raw []byte, checks []checkRow) ([]models.PillarScore, error) {
	catalog, err := loadPillarCatalog(ctx, s)
	if err != nil {
		return nil, err
	}
	stored := parsePillarStats(raw)
	byPillar := map[string][]checkRow{}
	for _, c := range checks {
		byPillar[c.Pillar] = append(byPillar[c.Pillar], c)
	}
	out := make([]models.PillarScore, 0, len(catalog))
	for _, pm := range catalog {
		st, haveStored := stored[pm.Code]
		rows := byPillar[pm.Code]
		if !haveStored && len(rows) == 0 {
			continue
		}
		var failing []checkRow
		var lost, penalty float64
		for _, c := range rows {
			penalty += c.Penalty
			if strings.EqualFold(c.Status, "FAIL") && c.PointsLost > 0 {
				failing = append(failing, c)
				lost += c.PointsLost
			}
		}
		p := models.PillarScore{Pillar: pm.Code, Name: pm.Name, Status: st.Status}
		if st.Score != nil {
			v := round1(*st.Score)
			p.Score = &v
		} else if penalty > 0 {
			v := round1(100 * (1 - lost/penalty))
			if v < 0 {
				v = 0
			}
			p.Score = &v
		}
		p.Evaluated, p.Failing = len(rows), len(failing)
		if st.Evaluated != nil {
			p.Evaluated = *st.Evaluated
		}
		if st.Failing != nil {
			p.Failing = *st.Failing
		}
		if p.Status == "" {
			p.Status = identityPillarStatus(failing)
		}
		out = append(out, p)
	}
	return out, nil
}

// peerGroup compares the identity with the others of its functional class.
// rank 1 is the highest score.
func (r *IdentityPostureRepository) peerGroup(ctx context.Context, s *scope.DataScope, id, class string, score sql.NullFloat64) (*models.PeerGroup, error) {
	table := s.Table(tIdentityPosture)
	q := fmt.Sprintf(`SELECT count(*)::int, COALESCE(avg(score), 0), COALESCE(max(score), 0),
			count(*) FILTER (WHERE score > (SELECT score FROM %[1]s WHERE tenant_id = $1 AND identity_id = $3))::int
		FROM %[1]s WHERE tenant_id = $1 AND functional_class = $2`, table)
	pg := &models.PeerGroup{FunctionalClass: class}
	var avg, best float64
	var higher int
	if err := s.DB.QueryRowContext(ctx, q, s.TenantID, class, id).Scan(&pg.Size, &avg, &best, &higher); err != nil {
		return nil, fmt.Errorf("load peer group: %w", err)
	}
	pg.Average, pg.Best, pg.Rank = round1(avg), round1(best), higher+1
	pg.Standing = standing(pg.Size, pg.Rank, score, avg)
	return pg, nil
}

func standing(size, rank int, score sql.NullFloat64, avg float64) string {
	switch {
	case size <= 1:
		return "Only identity in group"
	case rank == 1:
		return "Highest in group"
	case rank == size:
		return "Lowest in group"
	case score.Valid && score.Float64 >= avg:
		return "Above group average"
	}
	return "Below group average"
}

// boundCredentials lists the credentials the identity is bound to (BOUND_TO
// edges), with the latest known row of each credential (see credentialSource).
func (r *IdentityPostureRepository) boundCredentials(ctx context.Context, s *scope.DataScope, arn string) ([]models.BoundCredential, error) {
	src, err := credentialSource(ctx, s)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT e.target_arn, COALESCE(e.target_type, ''), COALESCE(c.credential_type, ''), COALESCE(c.name, ''),
			c.created_at_aws, c.last_used_at
		FROM %s e
		%s
		WHERE e.tenant_id = $1 AND e.source_arn = $2 AND e.edge_type = 'BOUND_TO'
		ORDER BY e.target_arn`, s.Table(tAccessEdges), latestCredential(src))
	rows, err := s.DB.QueryContext(ctx, q, s.TenantID, arn)
	if err != nil {
		if isUndefinedTable(err) {
			return []models.BoundCredential{}, nil
		}
		return nil, fmt.Errorf("load bound credentials: %w", err)
	}
	defer rows.Close()
	out := make([]models.BoundCredential, 0)
	for rows.Next() {
		var target, edgeType, credType, name string
		var created, used sql.NullTime
		if err := rows.Scan(&target, &edgeType, &credType, &name, &created, &used); err != nil {
			return nil, fmt.Errorf("scan bound credential: %w", err)
		}
		bc := models.BoundCredential{Type: strings.ToUpper(firstNonEmpty(credType, edgeType)), ID: credentialID(name, target)}
		bc.AgeDays, bc.LastUsedDays = daysSince(created), daysSince(used)
		bc.Status = usageState(used, bc.LastUsedDays)
		out = append(out, bc)
	}
	return out, rows.Err()
}

// usageState: never used, unused for a long time, or in use.
func usageState(lastUsed sql.NullTime, days *int) string {
	switch {
	case !lastUsed.Valid:
		return "Never used"
	case days != nil && *days > unusedAfterDays:
		return "Unused"
	}
	return "In use"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// credentialID is the access-key id for keys (the part of the ARN after
// "access-key/"), else the credential name, else the ARN.
func credentialID(name, arn string) string {
	if id, ok := accessKeyID(arn); ok {
		return id
	}
	if strings.TrimSpace(name) != "" {
		return name
	}
	return arn
}
