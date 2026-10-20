package credential_exposure

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	svc "nhi-backend-api/internal/services/credential_exposure"
)

func TestWriteAllowlistError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAllowlistError(rec, "upsert", &svc.InvalidInputError{Msg: "reason exceeds 1000 characters"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "reason exceeds 1000 characters") {
		t.Fatalf("validation: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	writeAllowlistError(rec, "upsert", errors.New(`open tenant database "secretdb": pq: boom`))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secretdb") {
		t.Fatalf("internal error leaked or wrong status: %d %s", rec.Code, rec.Body.String())
	}
}
