package controllers

import (
	"errors"
	"net/http"
	"strconv"
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
	filter, err := itemFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, err := c.svc.List(r.Context(), limit, offset, filter)
	respond(w, http.StatusOK, page, err)
}

// itemFilter parses and whitelists the GET /items query parameters.
// Supported: q, priority, status, workflow_id, stage_id, assigned_to,
// sort (created_at|updated_at|title|priority), order (asc|desc).
func itemFilter(r *http.Request) (dto.ItemFilter, error) {
	p, err := listParams(r, "created_at", "updated_at", "title", "priority", "status", "version")
	if err != nil {
		return dto.ItemFilter{}, err
	}
	q := r.URL.Query()
	f := dto.ItemFilter{
		Search:     p.Search,
		Sort:       p.Sort,
		Order:      p.Order,
		Priority:   q.Get("priority"),
		Status:     q.Get("status"),
		WorkflowID: q.Get("workflow_id"),
		StageID:    q.Get("stage_id"),
		AssignedTo: q.Get("assigned_to"),
	}
	switch f.Priority {
	case "", "urgent", "high", "medium", "low":
	default:
		return f, errors.New("invalid priority filter")
	}
	switch f.Status {
	case "", "open", "closed":
	default:
		return f, errors.New("invalid status filter")
	}
	return f, nil
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

// Timeline returns the merged item timeline (history + stage activity).
// GET /items/{id}/timeline?before_version=&limit=
func (c *ItemController) Timeline(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var before *int64
	if raw := r.URL.Query().Get("before_version"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 1 {
			http.Error(w, "invalid before_version", http.StatusBadRequest)
			return
		}
		before = &v
	}
	page, err := c.svc.Timeline(r.Context(), r.PathValue("id"), before, limit)
	respond(w, http.StatusOK, page, err)
}

// AdvanceStage moves the item to the next stage in its workflow.
// Caller must be an org admin or a leader of the current stage's team.
// POST /items/{id}/advance
func (c *ItemController) AdvanceStage(w http.ResponseWriter, r *http.Request) {
	req, ok := versionRequest(w, r)
	if !ok {
		return
	}
	i, err := c.svc.AdvanceStage(r.Context(), r.PathValue("id"), req.Version)
	respond(w, http.StatusOK, i, err)
}

// SetStatus closes or reopens the item.
// Caller must be an org admin or a leader of the current stage's team.
// POST /items/{id}/status
func (c *ItemController) SetStatus(w http.ResponseWriter, r *http.Request) {
	var req dto.SetStatusRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Version < 1 {
		http.Error(w, "version is required and must be >= 1", http.StatusBadRequest)
		return
	}
	i, err := c.svc.SetClosed(r.Context(), r.PathValue("id"), req.Closed, req.Version)
	respond(w, http.StatusOK, i, err)
}

// ClaimActivity lets an org admin, editor, or team leader self-assign the open
// unclaimed activity for this item.
// POST /items/{id}/activity/claim
func (c *ItemController) ClaimActivity(w http.ResponseWriter, r *http.Request) {
	req, ok := versionRequest(w, r)
	if !ok {
		return
	}
	act, err := c.svc.ClaimActivity(r.Context(), r.PathValue("id"), req.Version)
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
	if req.Version < 1 {
		http.Error(w, "version is required and must be >= 1", http.StatusBadRequest)
		return
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		http.Error(w, "note is required", http.StatusBadRequest)
		return
	}
	act, err := c.svc.CompleteActivity(r.Context(), r.PathValue("id"), note, req.Version)
	respond(w, http.StatusOK, act, err)
}

// ApproveActivity approves or rejects a pending activity.
// The caller must be the stage approver, a team leader, or an org admin.
// POST /items/{id}/activity/approve
func (c *ItemController) ApproveActivity(w http.ResponseWriter, r *http.Request) {
	var req dto.ApproveActivityRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Version < 1 {
		http.Error(w, "version is required and must be >= 1", http.StatusBadRequest)
		return
	}
	act, err := c.svc.ApproveActivity(r.Context(), r.PathValue("id"), req.Approved, req.Note, req.Version)
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
	if req.Version < 1 {
		http.Error(w, "version is required and must be >= 1", http.StatusBadRequest)
		return
	}
	act, err := c.svc.ReassignActivity(r.Context(), r.PathValue("id"), req.UserID, req.Version)
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

// Attention returns the caller's dashboard "what needs my attention" summary.
// GET /items/attention
func (c *ItemController) Attention(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 20 {
		limit = 5
	}
	a, err := c.svc.Attention(r.Context(), limit)
	respond(w, http.StatusOK, a, err)
}

// versionRequest decodes a body carrying only the expected item version.
func versionRequest(w http.ResponseWriter, r *http.Request) (dto.VersionRequest, bool) {
	var req dto.VersionRequest
	if !decode(w, r, &req) {
		return req, false
	}
	if req.Version < 1 {
		http.Error(w, "version is required and must be >= 1", http.StatusBadRequest)
		return req, false
	}
	return req, true
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
