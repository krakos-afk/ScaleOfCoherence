package teams

import (
	"context"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
)

type Service struct {
	repo *repository.TeamRepo
}

func NewService(repo *repository.TeamRepo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, name string) (dto.Team, error) {
	id, err := admin(ctx)
	if err != nil {
		return dto.Team{}, err
	}
	return s.repo.Create(ctx, id.OrgID, name)
}

func (s *Service) List(ctx context.Context, limit, offset int, p dto.ListParams) (dto.Page[dto.Team], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.Team]{}, err
	}
	return s.repo.List(ctx, id.OrgID, limit, offset, p)
}

func (s *Service) Get(ctx context.Context, teamID string) (dto.Team, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Team{}, err
	}
	return s.repo.Get(ctx, id.OrgID, teamID)
}

// Update allows org-admins unconditionally, or a leader who is a member of
// the team being updated.
func (s *Service) Update(ctx context.Context, teamID, name string) (dto.Team, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Team{}, err
	}
	if !id.IsAdmin() {
		if err := s.requireTeamLeader(ctx, id, teamID); err != nil {
			return dto.Team{}, err
		}
	}
	return s.repo.Update(ctx, id.OrgID, teamID, name)
}

// Delete is restricted to org-admins only.
func (s *Service) Delete(ctx context.Context, teamID string) error {
	id, err := admin(ctx)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, id.OrgID, teamID)
}

// ListMembers is readable by any authenticated org member.
func (s *Service) ListMembers(ctx context.Context, teamID string, limit, offset int, p dto.ListParams) (dto.Page[dto.TeamMember], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.TeamMember]{}, err
	}
	if _, err := s.repo.Get(ctx, id.OrgID, teamID); err != nil {
		return dto.Page[dto.TeamMember]{}, err
	}
	return s.repo.ListMembers(ctx, id.OrgID, teamID, limit, offset, p)
}

// AddMember allows org-admins unconditionally, or a leader of the target team.
func (s *Service) AddMember(ctx context.Context, teamID, userID, role string) error {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return err
	}
	if !id.IsAdmin() {
		if err := s.requireTeamLeader(ctx, id, teamID); err != nil {
			return err
		}
	}
	return s.repo.AddMember(ctx, id.OrgID, teamID, userID, role)
}

// RemoveMember allows org-admins unconditionally, or a leader of the target team.
func (s *Service) RemoveMember(ctx context.Context, teamID, userID string) error {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return err
	}
	if !id.IsAdmin() {
		if err := s.requireTeamLeader(ctx, id, teamID); err != nil {
			return err
		}
	}
	return s.repo.RemoveMember(ctx, id.OrgID, teamID, userID)
}

// requireTeamLeader returns ErrForbidden unless the caller holds the "leader"
// role in the given team (within their org).
func (s *Service) requireTeamLeader(ctx context.Context, id identity.Identity, teamID string) error {
	role, err := s.repo.GetMemberRole(ctx, id.OrgID, teamID, id.UserID)
	if err != nil {
		return auth.ErrForbidden
	}
	if role != "leader" {
		return auth.ErrForbidden
	}
	return nil
}

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
