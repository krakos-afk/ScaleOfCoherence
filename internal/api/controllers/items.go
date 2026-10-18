package controllers

import (
	"net/http"
	"strings"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/items"
)

type ItemController struct {
	svc *items.Service
}

func NewItemController(svc *items.Service) *ItemController {
	return &ItemController{svc: svc}
}

func (c *ItemController) Create(w http.ResponseWriter, r *http.Request) {
	req, ok := itemRequest(w, r)
	if !ok {
		return
	}
	i, err := c.svc.Create(r.Context(), req)
	respond(w, http.StatusCreated, i, err)
}

func (c *ItemController) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	page, err := c.svc.List(r.Context(), limit, offset)
	respond(w, http.StatusOK, page, err)
}

func (c *ItemController) Get(w http.ResponseWriter, r *http.Request) {
	i, err := c.svc.Get(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, i, err)
}

func (c *ItemController) Update(w http.ResponseWriter, r *http.Request) {
	req, ok := itemRequest(w, r)
	if !ok {
		return
	}
	if req.Version < 1 {
		http.Error(w, "version is required and must be >= 1", http.StatusBadRequest)
		return
	}
	i, err := c.svc.Update(r.Context(), r.PathValue("id"), req)
	respond(w, http.StatusOK, i, err)
}

func (c *ItemController) Delete(w http.ResponseWriter, r *http.Request) {
	noContent(w, c.svc.Delete(r.Context(), r.PathValue("id")))
}

// ListChanges returns the version-change history for one item.
// GET /items/{id}/changes
func (c *ItemController) ListChanges(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	page, err := c.svc.ListChanges(r.Context(), r.PathValue("id"), limit, offset)
	respond(w, http.StatusOK, page, err)
}

// ListActivity returns the stage-activity history for one item.
// GET /items/{id}/activity
func (c *ItemController) ListActivity(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	page, err := c.svc.ListActivity(r.Context(), r.PathValue("id"), limit, offset)
	respond(w, http.StatusOK, page, err)
}

// ClaimActivity lets a team member (editor or leader) self-assign the open
// unclaimed activity for this item.
// POST /items/{id}/activity/claim
func (c *ItemController) ClaimActivity(w http.ResponseWriter, r *http.Request) {
	act, err := c.svc.ClaimActivity(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, act, err)
}

// CompleteActivity marks the open activity as done (or pending approval).
// The caller must be the currently assigned user.
// POST /items/{id}/activity/complete
func (c *ItemController) CompleteActivity(w http.ResponseWriter, r *http.Request) {
	var req dto.CompleteActivityRequest
	if !decode(w, r, &req) {
		return
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		http.Error(w, "note is required", http.StatusBadRequest)
		return
	}
	act, err := c.svc.CompleteActivity(r.Context(), r.PathValue("id"), note)
	respond(w, http.StatusOK, act, err)
}

// ApproveActivity approves or rejects a pending activity.
// The caller must be the stage approver or a team leader.
// POST /items/{id}/activity/approve
func (c *ItemController) ApproveActivity(w http.ResponseWriter, r *http.Request) {
	var req dto.ApproveActivityRequest
	if !decode(w, r, &req) {
		return
	}
	act, err := c.svc.ApproveActivity(r.Context(), r.PathValue("id"), req.Approved, req.Note)
	respond(w, http.StatusOK, act, err)
}

// ReassignActivity releases the current assignee and opens a new activity row
// for the given user. Caller must be an org admin or a team leader.
// POST /items/{id}/activity/reassign
func (c *ItemController) ReassignActivity(w http.ResponseWriter, r *http.Request) {
	var req dto.ReassignActivityRequest
	if !decode(w, r, &req) {
		return
	}
	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	act, err := c.svc.ReassignActivity(r.Context(), r.PathValue("id"), req.UserID)
	respond(w, http.StatusOK, act, err)
}

// GetOpenActivity returns the current open activity for an item.
// GET /items/{id}/activity/open
func (c *ItemController) GetOpenActivity(w http.ResponseWriter, r *http.Request) {
	act, err := c.svc.GetOpenActivity(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, act, err)
}

// ListMy returns items where the caller has an open or pending activity.
// GET /items/my
func (c *ItemController) ListMy(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	page, err := c.svc.ListMy(r.Context(), limit, offset)
	respond(w, http.StatusOK, page, err)
}

func itemRequest(w http.ResponseWriter, r *http.Request) (dto.ItemRequest, bool) {
	var req dto.ItemRequest
	if !decode(w, r, &req) {
		return req, false
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}
	switch req.Priority {
	case "urgent", "high", "medium", "low":
	default:
		req.Title = ""
	}
	if req.Title == "" {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return req, false
	}
	return req, true
}
