// Package common holds the pieces shared by every genome (NHI monitoring)
// endpoint: the response envelope, request/claims/query helpers, the
// normalized monitoring identity and its lookup, the Postgres/ClickHouse
// error classification, and the configurable Genome ClickHouse event reader.
// Each endpoint
// lives in its own sub-package (one per API) with its own handler, service,
// repository and DTO files, and imports this package instead of each other —
// mirroring internal/modules/posture's common package.
//
// This endpoint answers in its own documented envelope — {data, meta} on
// success, {error} on failure, both carrying a request id — not in
// platform/httpx.APIResponse.
package common

import "time"

// Envelope is the success body: {data, meta}.
type Envelope struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// Meta is the meta block. NextCursor is set only by paged endpoints.
type Meta struct {
	RequestID   string    `json:"request_id"`
	GeneratedAt time.Time `json:"generated_at"`
	NextCursor  *string   `json:"next_cursor"`
}

// ErrorEnvelope is the error body: {error: {code, message, request_id}}.
type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes one failure.
type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// Error codes, in this API's UPPER_SNAKE style.
const (
	CodeValidation           = "VALIDATION_ERROR"       // 400: bad path/query/body
	CodeUnauthorized         = "UNAUTHORIZED"           // 401: no usable token claims
	CodePayloadTooLarge      = "PAYLOAD_TOO_LARGE"      // 413: request body over the limit
	CodeUnsupportedMediaType = "UNSUPPORTED_MEDIA_TYPE" // 415: body is not application/json
	CodeInternal             = "INTERNAL_ERROR"         // 500: unexpected failure (details only in the server log)
	CodeStoreUnavailable     = "STORE_UNAVAILABLE"      // 503: tenant database/table cannot be used
	CodeStoreTimeout         = "STORE_TIMEOUT"          // 504: the request's time budget ran out
	CodeIdentityNotFound     = "IDENTITY_NOT_FOUND"     // 404: unknown {source_type}/{identity_name}
	CodeAnomalyNotFound      = "ANOMALY_NOT_FOUND"      // 404: unknown alert id
	CodeNotImplemented       = "NOT_IMPLEMENTED"        // 501: action the current pipeline cannot perform
)
