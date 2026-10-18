package controllers

import (
	"net/http"

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
	i, err := c.svc.Update(r.Context(), r.PathValue("id"), req)
	respond(w, http.StatusOK, i, err)
}

func (c *ItemController) Delete(w http.ResponseWriter, r *http.Request) {
	noContent(w, c.svc.Delete(r.Context(), r.PathValue("id")))
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
