package dto

import "time"

// CommentRequest is the body for POST /items/{id}/comments. The item version
// the comment is anchored to is taken server-side, not from the client.
type CommentRequest struct {
	Body string `json:"body"`
}

// Comment is one item comment. Version is the item's current version when the
// comment was created; it always references a logged item change.
type Comment struct {
	ID        int64     `json:"id"`
	ItemID    string    `json:"item_id"`
	AuthorID  string    `json:"author_id"`
	Author    string    `json:"author"`
	Version   int64     `json:"version"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}
