package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type PostureExportRepository struct {
	list *IdentitiesListRepository
}

func NewPostureExportRepository() *PostureExportRepository {
	return &PostureExportRepository{list: NewIdentitiesListRepository()}
}

// Stream implements GET /posture/export. It walks the filtered identities in
// keyset order, models.ExportChunkSize rows per query, and hands each chunk to
// emit as soon as it is read, so memory stays flat for any fleet size. emit
// returning an error (for example a closed connection) stops the walk.
func (r *PostureExportRepository) Stream(ctx context.Context, s *scope.DataScope, f models.IdentityFilter, emit func([]models.ExportRow) error) error {
	if err := need(s); err != nil {
		return err
	}
	if f.Sort == "" {
		f.Sort = models.SortScore
	}
	afterID := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := r.list.queryIdentityPage(ctx, s, f, afterID, models.ExportChunkSize)
		if err != nil {
			return fmt.Errorf("export chunk: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		chunk := make([]models.ExportRow, 0, len(rows))
		for _, row := range rows {
			chunk = append(chunk, row.toExport())
		}
		if err := emit(chunk); err != nil {
			return err
		}
		if len(rows) < models.ExportChunkSize {
			return nil
		}
		afterID = rows[len(rows)-1].IdentityID
	}
}

func (row IdentityRow) toExport() models.ExportRow {
	account := row.AccountName
	if account == "" {
		account = row.AccountID
	}
	e := models.ExportRow{
		IdentityID:        row.IdentityID,
		Name:              row.Name,
		Type:              row.Type,
		Account:           account,
		Score:             nullFloat(row.Score),
		Band:              row.Band,
		Grade:             row.Grade,
		ChecksFailed:      intVal(row.Failed),
		FailedCritical:    intVal(row.Critical),
		FailedHigh:        intVal(row.High),
		FailedMedium:      intVal(row.Medium),
		FailedLow:         intVal(row.Low),
		CredentialAgeDays: nullInt(row.CredAgeDays),
		Change:            nullFloat(row.Change),
		OpenAlerts:        intVal(row.OpenAlerts),
	}
	if row.Class.Valid {
		e.FunctionalClass = row.Class.String
	}
	if row.WeakestPillar.Valid {
		e.WeakestPillar = row.WeakestPillar.String
	}
	if row.CredType.Valid {
		e.CredentialType = row.CredType.String
	}
	return e
}
