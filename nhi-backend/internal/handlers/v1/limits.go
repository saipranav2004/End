package v1

// Request-limit constants shared by every v1 handler, so the documented
// contract is enforced in exactly one place.
const (
	// maxPageSize is the documented ceiling for ?page_size= (PDF: "Rows per
	// page (max 100)"). Values above it are clamped, not rejected.
	maxPageSize = 100

	// maxRelationshipDepth is the documented ceiling for ?depth= on the
	// relationships tab (PDF: "Hops: 1, 2, 3"). Values outside 1..3 are
	// rejected with 400 so the client knows what it actually received.
	maxRelationshipDepth = 3
)
