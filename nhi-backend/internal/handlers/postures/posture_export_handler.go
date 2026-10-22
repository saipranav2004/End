package postures

import (
	"encoding/csv"
	"net/http"
	"nhi-backend-api/internal/logging"
	"strconv"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/postures"
	svc "nhi-backend-api/internal/services/postures"
)

type PostureExportHandler struct {
	service *svc.PostureExportService
}

func NewPostureExportHandler(service *svc.PostureExportService) *PostureExportHandler {
	return &PostureExportHandler{service: service}
}

// exportChunkDeadline is the write time allowed per 1,000-row chunk. The
// server-wide write timeout would otherwise cut a long export off.
const exportChunkDeadline = 60 * time.Second

// Get handles GET /posture/export?format=csv&window=30d&<identity filters> —
// every identity's posture as a CSV file, streamed in chunks.
func (h *PostureExportHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if !strictQuery(w, q, allow(append([]string{"format"}, identityFilterParams...)...)) {
		return
	}
	if f := strings.ToLower(strings.TrimSpace(q.Get("format"))); f != "" && f != "csv" {
		invalid(w, "format", "format must be csv")
		return
	}
	filter, ok := parseIdentityFilter(w, q, false)
	if !ok {
		return
	}

	var cw *csv.Writer
	started := false
	ctl := http.NewResponseController(w)
	start := func() {
		name := "posture-" + filter.Window.Label + "-" + time.Now().UTC().Format("20060102") + ".csv"
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(http.StatusOK)
		cw = csv.NewWriter(w)
		_ = cw.Write(models.ExportColumns(filter.Window))
		started = true
	}

	err := h.service.Stream(r.Context(), c, filter, func(rows []models.ExportRow) error {
		if !started {
			start()
		}
		_ = ctl.SetWriteDeadline(time.Now().Add(exportChunkDeadline))
		for _, row := range rows {
			if err := cw.Write(exportRecord(row)); err != nil {
				return err
			}
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			return err
		}
		_ = ctl.Flush()
		return nil
	})
	if err != nil {
		if !started {
			fail(w, r, "export posture", err)
			return
		}
		logging.FromContext(r.Context()).Warn("posture export interrupted", "err", err)
		return
	}
	if !started { // no identity matched: header line only
		start()
		cw.Flush()
	}
}

func exportRecord(r models.ExportRow) []string {
	return []string{
		r.IdentityID, safeCell(r.Name), r.Type, safeCell(r.FunctionalClass), safeCell(r.Account),
		num(r.Score), r.Band, r.Grade,
		strconv.Itoa(r.ChecksFailed), strconv.Itoa(r.FailedCritical), strconv.Itoa(r.FailedHigh),
		strconv.Itoa(r.FailedMedium), strconv.Itoa(r.FailedLow),
		r.WeakestPillar, r.CredentialType, intPtr(r.CredentialAgeDays), num(r.Change), strconv.Itoa(r.OpenAlerts),
	}
}

func num(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'f', -1, 64)
}

func intPtr(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}

// safeCell stops spreadsheet formula injection: a text cell that starts with
// =, +, -, @, tab or carriage return is prefixed with a quote.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
