package credential_exposure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nhi-backend-api/internal/scope"
)

var (
	ErrDecisionFindingNotFound  = errors.New("finding not found")
	ErrDecisionFindingAmbiguous = errors.New("finding id exists in both platform tables")
)

type DecisionRepository struct{}

func NewDecisionRepository() *DecisionRepository { return &DecisionRepository{} }

func (r *DecisionRepository) Save(ctx context.Context, resolved *scope.DataScope, findingID, decision, decidedBy string) error {
	if resolved == nil || resolved.DB == nil {
		return errScopeRequired
	}
	schema := resolved.QSchema()
	tx, err := resolved.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finding decision: %w", err)
	}
	defer tx.Rollback()
	var cc, gh bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+schema+`.secscan_findings_codecommit WHERE finding_id=$1), EXISTS(SELECT 1 FROM `+schema+`.secscan_findings_github WHERE finding_id=$1)`, findingID).Scan(&cc, &gh); err != nil {
		return fmt.Errorf("locate finding for decision: %w", err)
	}
	if !cc && !gh {
		return ErrDecisionFindingNotFound
	}
	if cc && gh {
		return ErrDecisionFindingAmbiguous
	}
	table := schema + ".secscan_findings_codecommit"
	if gh {
		table = schema + ".secscan_findings_github"
	}
	query := `UPDATE ` + table + ` SET analyst_decision=$1,analyst_decided_at=$2,analyst_decided_by=$3 WHERE finding_id=$4`
	res, err := tx.ExecContext(ctx, query, decision, dashboardTimestamp(time.Now()), decidedBy, findingID)
	if err != nil {
		return fmt.Errorf("save analyst decision: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify analyst decision update: %w", err)
	}
	if n != 1 {
		return ErrDecisionFindingNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit analyst decision: %w", err)
	}
	return nil
}
