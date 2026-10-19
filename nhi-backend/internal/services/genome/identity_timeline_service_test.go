package genome

import (
	"testing"
	"time"

	repo "nhi-backend-api/internal/repository/genome"
)

func TestAnomalyEntriesGroupsPerWindow(t *testing.T) {
	w1 := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)
	w0 := w1.Add(-time.Hour)
	alerts := []repo.TimelineAlertRow{ // newest first, as the repository returns them
		{ID: 9, Dimension: "api_operation", Reason: "new operation", Status: "open", WindowEnd: w1, CreatedAt: w1.Add(time.Minute)},
		{ID: 8, Dimension: "api_operation", Reason: "rare operation", Status: "open", WindowEnd: w1, CreatedAt: w1.Add(time.Minute)},
		{ID: 7, Dimension: "region", Reason: "new region", Status: "open", WindowEnd: w1, CreatedAt: w1},
		{ID: 5, Dimension: "api_operation", Reason: "new operation", Status: "resolved", WindowEnd: w0, CreatedAt: w0},
	}
	got := anomalyEntries(alerts)
	if len(got) != 3 {
		t.Fatalf("got %d items, want 3: %+v", len(got), got)
	}
	first := got[0].item
	if first.Title != "2 active anomalies" || !first.OccurredAt.Equal(w1) || *first.Status != "open" ||
		*first.Description != "Dimension: api_operation; new operation (and 1 other reason)" || got[0].key.Ref != "9" {
		t.Errorf("first item = %+v (%s / %s), key %+v", first, *first.Description, *first.Status, got[0].key)
	}
	if got[1].item.Title != "1 active anomaly" || *got[1].item.Description != "Dimension: region; new region" {
		t.Errorf("second item = %s / %s", got[1].item.Title, *got[1].item.Description)
	}
	if got[2].item.Title != "1 resolved anomaly" || !got[2].item.OccurredAt.Equal(w0) {
		t.Errorf("third item = %s at %s", got[2].item.Title, got[2].item.OccurredAt)
	}
}
