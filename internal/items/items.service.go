package items

import (
	"context"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
)

type Service struct {
	repo *repository.ItemRepo
}

func NewService(repo *repository.ItemRepo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, req dto.ItemRequest) (dto.Item, error) {
	id, err := writer(ctx)
	if err != nil {
		return dto.Item{}, err
	}
	return s.repo.Create(ctx, id.OrgID, id.UserID, req)
}

func (s *Service) List(ctx context.Context, limit, offset int, filter dto.ItemFilter) (dto.Page[dto.Item], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.Item]{}, err
	}
	// "me" is resolved against the caller's token, not client input.
	if filter.AssignedTo == "me" {
		filter.AssignedTo = id.UserID
	}
	return s.repo.List(ctx, id.OrgID, limit, offset, filter)
}

func (s *Service) Get(ctx context.Context, itemID string) (dto.Item, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Item{}, err
	}
	return s.repo.Get(ctx, id.OrgID, itemID)
}

func (s *Service) Update(ctx context.Context, itemID string, req dto.ItemRequest) (dto.Item, error) {
	id, err := writer(ctx)
	if err != nil {
		return dto.Item{}, err
	}
	return s.repo.Update(ctx, id.OrgID, itemID, id.UserID, req)
}

func (s *Service) Delete(ctx context.Context, itemID string) error {
	id, err := admin(ctx)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, id.OrgID, itemID)
}

// Timeline returns one page of the merged item timeline (newest first).
func (s *Service) Timeline(ctx context.Context, itemID string, beforeVersion *int64, limit int) (dto.TimelinePage, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.TimelinePage{}, err
	}
	return s.repo.Timeline(ctx, id.OrgID, itemID, beforeVersion, limit)
}

// AdvanceStage moves the item to the next stage in its workflow.
// Caller must be an org admin or a leader of the current stage's team.
func (s *Service) AdvanceStage(ctx context.Context, itemID string, version int64) (dto.Item, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Item{}, err
	}
	return s.repo.AdvanceStage(ctx, id.OrgID, itemID, id.UserID, version, id.IsAdmin())
}

// SetClosed closes or reopens the item.
// Caller must be an org admin or a leader of the current stage's team.
func (s *Service) SetClosed(ctx context.Context, itemID string, closed bool, version int64) (dto.Item, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Item{}, err
	}
	return s.repo.SetClosed(ctx, id.OrgID, itemID, id.UserID, closed, version, id.IsAdmin())
}

// ClaimActivity lets a team member (editor or leader) or an org admin
// self-assign the open unclaimed activity for the item at their current stage.
func (s *Service) ClaimActivity(ctx context.Context, itemID string, version int64) (dto.ItemStageActivity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return s.repo.ClaimActivity(ctx, id.OrgID, itemID, id.UserID, version, id.IsAdmin())
}

// CompleteActivity marks the open activity as completed (or pending approval).
// Only the currently assigned user may call this.
func (s *Service) CompleteActivity(ctx context.Context, itemID, note string, version int64) (dto.ItemStageActivity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return s.repo.CompleteActivity(ctx, id.OrgID, itemID, id.UserID, note, version)
}

// ApproveActivity approves or rejects a pending activity.
// Caller must be the stage approver, a team leader, or an org admin.
func (s *Service) ApproveActivity(ctx context.Context, itemID string, approved bool, note string, version int64) (dto.ItemStageActivity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return s.repo.ApproveActivity(ctx, id.OrgID, itemID, id.UserID, approved, note, version, id.IsAdmin())
}

// ReassignActivity releases the current assignee and opens a new activity for
// targetUserID. Caller must be an org admin or a team leader.
func (s *Service) ReassignActivity(ctx context.Context, itemID, targetUserID string, version int64) (dto.ItemStageActivity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	// callerIsAdmin is forwarded so the repo can skip the team-leader check.
	return s.repo.ReassignActivity(ctx, id.OrgID, itemID, id.UserID, targetUserID, version, id.IsAdmin())
}

// GetOpenActivity returns the current open activity for an item.
func (s *Service) GetOpenActivity(ctx context.Context, itemID string) (dto.ItemStageActivity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return s.repo.GetOpenActivity(ctx, id.OrgID, itemID)
}

// ListMy returns items where the caller has an open or pending activity.
func (s *Service) ListMy(ctx context.Context, limit, offset int) (dto.Page[dto.Item], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.Item]{}, err
	}
	return s.repo.ListMy(ctx, id.OrgID, id.UserID, limit, offset)
}

// Attention returns the caller's dashboard attention summary.
func (s *Service) Attention(ctx context.Context, limit int) (dto.Attention, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Attention{}, err
	}
	return s.repo.Attention(ctx, id.OrgID, id.UserID, limit)
}

// writer returns the identity if the caller may create/update items.
// Any org member (admin or user) may; viewer/editor restrictions are
// enforced per-team when working on item activity.
func writer(ctx context.Context) (identity.Identity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return id, err
	}
	if !id.CanWrite() {
		return id, auth.ErrForbidden
	}
	return id, nil
}

// admin returns the identity if the caller is an org admin.
// Used for destructive ops (delete item); team leaders operate on item
// activity through the team-scoped claim/approve/reassign repo checks.
func admin(ctx context.Context) (identity.Identity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return id, err
	}
	if !id.IsAdmin() {
		return id, auth.ErrForbidden
	}
	return id, nil
}
