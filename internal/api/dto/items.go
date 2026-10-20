package dto

import (
	"encoding/json"
	"time"
)

// ItemFilter carries the list filters for GET /items. Empty fields are
// ignored. AssignedTo may be a user id, "me" (resolved by the service), or
// "unassigned" for items whose open activity has no assignee.
type ItemFilter struct {
	Search     string // substring match on title/description (trigram-backed ILIKE)
	Priority   string // urgent | high | medium | low
	Status     string // open | closed
	WorkflowID string
	StageID    string // current stage
	AssignedTo string // user id | me | unassigned
	Sort       string // created_at | updated_at | title | priority
	Order      string // asc | desc
}

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
	Note    string `json:"note"`
	Version int64  `json:"version"`
}

// ApproveActivityRequest is the body for POST /items/{id}/activity/approve.
type ApproveActivityRequest struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note"`
	Version  int64  `json:"version"`
}

// ReassignActivityRequest is the body for POST /items/{id}/activity/reassign.
type ReassignActivityRequest struct {
	UserID  string `json:"user_id"`
	Version int64  `json:"version"`
}

// SetStatusRequest is the body for POST /items/{id}/status.
type SetStatusRequest struct {
	Closed  bool  `json:"closed"`
	Version int64 `json:"version"`
}

// VersionRequest is the body for actions that only need an expected version
// (advance and claim).
type VersionRequest struct {
	Version int64 `json:"version"`
}

// TimelineUserRef is a display reference to a user in the timeline.
type TimelineUserRef struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// TimelineActivity is the activity state captured when a timeline entry was
// written. New entries carry an immutable snapshot in item_changes; legacy
// entries fall back to the current activity row.
type TimelineActivity struct {
	ID             int64            `json:"id"`
	Type           string           `json:"type,omitempty"`
	StageID        string           `json:"stage_id"`
	TeamID         string           `json:"team_id"`
	TeamName       string           `json:"team_name"`
	AssignedUserID *string          `json:"assigned_user_id"`
	AssignedUser   *TimelineUserRef `json:"assigned_user,omitempty"`
	CompletionNote *string          `json:"completion_note"`
	ApprovalStatus *string          `json:"approval_status"`
	ApprovalNote   *string          `json:"approval_note"`
}

// TimelineEntry is one merged row of the item timeline.
type TimelineEntry struct {
	Version   int64             `json:"version"`
	CreatedAt time.Time         `json:"created_at"`
	Actor     TimelineUserRef   `json:"actor"`
	Type      string            `json:"type"`
	Changes   json.RawMessage   `json:"changes"`
	Activity  *TimelineActivity `json:"activity,omitempty"`
}

// TimelinePage is the response for GET /items/{id}/timeline.
type TimelinePage struct {
	Entries           []TimelineEntry `json:"entries"`
	NextBeforeVersion *int64          `json:"next_before_version"`
}

// AttentionItem is a compact item summary for the dashboard attention widget.
// Reason is "approval" when the caller is the stage approver of a pending
// activity, or "assigned" when the caller holds the open activity.
type AttentionItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Priority  string `json:"priority"`
	StageName string `json:"stage_name"`
	Reason    string `json:"reason"`
}

// Attention is the response for GET /items/attention.
type Attention struct {
	AssignedTotal int             `json:"assigned_total"`
	ApprovalTotal int             `json:"approval_total"`
	Items         []AttentionItem `json:"items"`
}
