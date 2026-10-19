package models

// APIResponse is the standard wrapper for all API responses
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// PaginatedResponse wraps list endpoints with pagination info
type PaginatedResponse struct {
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
	Data       interface{} `json:"data"`
	TotalCount int         `json:"total_count"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
}
