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

func (s *Service) List(ctx context.Context, limit, offset int) (dto.Page[dto.Item], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.Item]{}, err
	}
	return s.repo.List(ctx, id.OrgID, limit, offset)
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
	id, err := adminOrLeader(ctx)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, id.OrgID, itemID)
}

func (s *Service) ListChanges(ctx context.Context, itemID string, limit, offset int) (dto.Page[dto.ItemChange], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.ItemChange]{}, err
	}
	return s.repo.ListChanges(ctx, id.OrgID, itemID, limit, offset)
}

func (s *Service) ListActivity(ctx context.Context, itemID string, limit, offset int) (dto.Page[dto.ItemStageActivity], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.ItemStageActivity]{}, err
	}
	return s.repo.ListActivity(ctx, id.OrgID, itemID, limit, offset)
}

// ClaimActivity lets an editor or leader team member self-assign the open
// unclaimed activity for the item at their current stage.
func (s *Service) ClaimActivity(ctx context.Context, itemID string) (dto.ItemStageActivity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return s.repo.ClaimActivity(ctx, id.OrgID, itemID, id.UserID)
}

// CompleteActivity marks the open activity as completed (or pending approval).
// Only the currently assigned user may call this.
func (s *Service) CompleteActivity(ctx context.Context, itemID, note string) (dto.ItemStageActivity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return s.repo.CompleteActivity(ctx, id.OrgID, itemID, id.UserID, note)
}

// ApproveActivity approves or rejects a pending activity.
// Caller must be the stage approver or a team leader.
func (s *Service) ApproveActivity(ctx context.Context, itemID string, approved bool, note string) (dto.ItemStageActivity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return s.repo.ApproveActivity(ctx, id.OrgID, itemID, id.UserID, approved, note)
}

// ReassignActivity releases the current assignee and opens a new activity for
// targetUserID. Caller must be an org admin or a team leader.
func (s *Service) ReassignActivity(ctx context.Context, itemID, targetUserID string) (dto.ItemStageActivity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	// callerIsAdmin is forwarded so the repo can skip the team-leader check.
	return s.repo.ReassignActivity(ctx, id.OrgID, itemID, id.UserID, targetUserID, id.IsAdmin())
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

// writer returns the identity if the caller may create/update items.
// admin, leader, and editor are all permitted; viewer is not.
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

// adminOrLeader returns the identity if the caller is admin or leader.
// Used for destructive ops (delete item) that editors should not perform.
func adminOrLeader(ctx context.Context) (identity.Identity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return id, err
	}
	if !id.CanManageTeam() {
		return id, auth.ErrForbidden
	}
	return id, nil
}
