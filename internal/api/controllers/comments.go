package controllers

import (
	"net/http"
	"strconv"
	"strings"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/comments"
)

type CommentController struct {
	svc *comments.Service
}

func NewCommentController(svc *comments.Service) *CommentController {
	return &CommentController{svc: svc}
}

// List returns a page of comments for an item (newest first).
// GET /items/{id}/comments?page=&limit=
func (c *CommentController) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	page, err := c.svc.List(r.Context(), r.PathValue("id"), limit, offset)
	respond(w, http.StatusOK, page, err)
}

// Create adds a comment anchored to the item's current version.
// Caller must be an org admin or an editor/commentator/leader in the team
// responsible for the item's current stage.
// POST /items/{id}/comments
func (c *CommentController) Create(w http.ResponseWriter, r *http.Request) {
	var req dto.CommentRequest
	if !decode(w, r, &req) {
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		http.Error(w, "comment body is required", http.StatusBadRequest)
		return
	}
	cm, err := c.svc.Create(r.Context(), r.PathValue("id"), body)
	respond(w, http.StatusCreated, cm, err)
}

// Delete removes a comment. Allowed for its author, org admins, and leaders of
// the team responsible for the item's current stage.
// DELETE /items/{id}/comments/{commentID}
func (c *CommentController) Delete(w http.ResponseWriter, r *http.Request) {
	commentID, err := strconv.ParseInt(r.PathValue("commentID"), 10, 64)
	if err != nil || commentID < 1 {
		http.Error(w, "invalid comment id", http.StatusBadRequest)
		return
	}
	noContent(w, c.svc.Delete(r.Context(), r.PathValue("id"), commentID))
}
