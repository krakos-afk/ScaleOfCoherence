package dto

type Page[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
}
