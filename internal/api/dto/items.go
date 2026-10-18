package dto

import (
	"encoding/json"
	"time"
)

// ItemRequest is the body for Create and Update.
// Version is required on Update for optimistic concurrency control.
// ClosedAt: send "close" to close the item, "reopen" to reopen it, or omit/empty to leave as-is.
type ItemRequest struct {
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Priority       string          `json:"priority"`
	WorkflowID     *string         `json:"workflow_id"`
	CurrentStageID *string         `json:"current_stage_id"`
	CustomFields   json.RawMessage `json:"custom_fields"`
	Close          *bool           `json:"close"`   // true = close item, false = reopen item, nil = leave unchanged
	Version        int64           `json:"version"` // must match current item version on Update
}

type Item struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Priority       string          `json:"priority"`
	CurrentStageID *string         `json:"current_stage_id"`
	WorkflowID     *string         `json:"workflow_id"`
	Version        int64           `json:"version"`
	CustomFields   json.RawMessage `json:"custom_fields"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	ClosedAt       *time.Time      `json:"closed_at"`
}

// ItemChange is one record in item_changes (a single version bump with a diff payload).
type ItemChange struct {
	ID        int64           `json:"id"`
	ItemID    string          `json:"item_id"`
	Version   int64           `json:"version"`
	ActorID   string          `json:"actor_id"`
	Changes   json.RawMessage `json:"changes"`
	CreatedAt time.Time       `json:"created_at"`
}

// ItemStageActivity is one record in item_stage_activity.
type ItemStageActivity struct {
	ID             int64      `json:"id"`
	ItemID         string     `json:"item_id"`
	StageID        string     `json:"stage_id"`
	TeamID         string     `json:"team_id"`
	AssignedUserID *string    `json:"assigned_user_id"` // nil = unclaimed
	AssignedBy     string     `json:"assigned_by"`
	AssignedAt     time.Time  `json:"assigned_at"`
	ItemVersion    int64      `json:"item_version"`
	CompletedBy    *string    `json:"completed_by"`
	CompletionNote *string    `json:"completion_note"`
	CompletedAt    *time.Time `json:"completed_at"`
	ReleasedAt     *time.Time `json:"released_at"`
	ReleaseReason  *string    `json:"release_reason"`
	ApprovalStatus *string    `json:"approval_status"`
	ApprovedBy     *string    `json:"approved_by"`
	ApprovedAt     *time.Time `json:"approved_at"`
	ApprovalNote   *string    `json:"approval_note"`
}

// CompleteActivityRequest is the body for POST /items/{id}/activity/complete.
type CompleteActivityRequest struct {
	Note string `json:"note"`
}

// ApproveActivityRequest is the body for POST /items/{id}/activity/approve.
type ApproveActivityRequest struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note"`
}

// ReassignActivityRequest is the body for POST /items/{id}/activity/reassign.
type ReassignActivityRequest struct {
	UserID string `json:"user_id"`
}
