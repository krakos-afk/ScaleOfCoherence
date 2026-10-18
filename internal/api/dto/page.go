package dto

type Page[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
}

// ListParams carries the shared search/sort/order options for list endpoints.
// Sort keys are whitelisted per endpoint; Order is "asc" or "desc".
type ListParams struct {
	Search string
	Sort   string
	Order  string
}
