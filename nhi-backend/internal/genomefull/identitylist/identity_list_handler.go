package identitylist

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves GET /api/v1/genome/identities (identity list and fleet
// table). It only reads the JWT claims, validates the request and shapes
// the response; the data work is in Service.
type Handler struct {
	service *Service
	now     func() time.Time
}

// NewHandler creates the handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters.
var allowed = map[string]struct{}{
	"source_type": {}, "search": {}, "account_id": {}, "region": {}, "status": {},
	"sort": {}, "limit": {}, "cursor": {},
}

// Get handles GET /api/v1/genome/identities.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "identity list", rid)
	ctx, cancel := common.RequestContext(r)
	defer cancel()

	claims, ok := common.RequireClaims(w, r, rid)
	if !ok {
		return
	}
	q := r.URL.Query()
	if unknown, bad := common.CheckUnknown(q, allowed); bad {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, common.UnknownParamMessage(q, unknown, allowed), rid)
		return
	}
	if err := common.CheckParamLimits(q); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}
	sortBy := strings.ToLower(strings.TrimSpace(q.Get("sort")))
	switch sortBy {
	case "":
		sortBy = SortRiskDesc
	case SortRiskDesc, SortLastSeenDesc, SortNameAsc:
	default:
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation,
			fmt.Sprintf("sort must be one of %s, %s, %s", SortRiskDesc, SortLastSeenDesc, SortNameAsc), rid)
		return
	}
	limit, err := common.ParseIdentityLimit(q, "limit")
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}

	data, next, err := h.service.List(ctx, claims.UserID, claims.TenantID, claims.DBName, Params{
		SourceType: strings.ToUpper(strings.TrimSpace(q.Get("source_type"))),
		Search:     strings.TrimSpace(q.Get("search")),
		AccountID:  strings.TrimSpace(q.Get("account_id")),
		Region:     strings.TrimSpace(q.Get("region")),
		Status:     strings.TrimSpace(q.Get("status")),
		Sort:       sortBy,
		Limit:      limit,
		Cursor:     strings.TrimSpace(q.Get("cursor")),
	})
	if errors.Is(err, common.ErrInvalidCursor) {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, "cursor is invalid", rid)
		return
	}
	if err != nil {
		common.WriteServiceError(w, r, "identity list", rid, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: data,
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now(), NextCursor: next},
	})
}
