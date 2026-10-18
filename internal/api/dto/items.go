package dto

import "time"

type ItemRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

type Item struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Priority    string    `json:"priority"`
	CreatedAt   time.Time `json:"created_at"`
}
