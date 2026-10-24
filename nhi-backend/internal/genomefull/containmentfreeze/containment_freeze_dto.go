// Package containmentfreeze serves
// POST /api/v1/genome/identities/{source_type}/{identity_name}/containment/freeze.
// The action must not run until a containment audit log and worker exist,
// so after validating the body and resolving the identity the endpoint
// answers 501 NOT_IMPLEMENTED.
package containmentfreeze

// Request is the request body.
type Request struct {
	Reason      string `json:"reason"`
	RequestedBy string `json:"requested_by"`
	DryRun      bool   `json:"dry_run"`
}
