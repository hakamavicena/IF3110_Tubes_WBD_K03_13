package accesscontrol

import (
	"context"

	"github.com/hakamavicena/if3110-dbengine/pkg/ports"
)

type AccessService struct {
	repo AccessRepository
}

func NewAccessService(repo AccessRepository) *AccessService {
	return &AccessService{repo: repo}
}

func (s *AccessService) GetEffectiveAccess(userID, dbID int) (AccessLevel, error) {
	user, err := s.repo.GetUser(userID)
	if err != nil {
		return AccessNone, err
	}
	if user.IsAdmin {
		return AccessViewEdit, nil
	}
	groups, err := s.repo.GetGroupsByUserID(userID)
	if err != nil {
		return AccessNone, err
	}
	effectiveAccess := AccessNone
	for _, group := range groups {
		level, err := s.repo.GetAccessByGroupAndDB(group.ID, dbID)
		if err == nil {
			if level > effectiveAccess {
				effectiveAccess = level
			}
		}
	}
	return effectiveAccess, nil
}

func (s *AccessService) RequirePlatformAdmin(userID int) error {
	user, err := s.repo.GetUser(userID)
	if err != nil {
		return err
	}
	if !user.IsAdmin {
		return ErrNotAdmin
	}
	return nil
}

func (s *AccessService) CanView(ctx context.Context, userID, databaseID int64) (bool, error) {
	access, err := s.GetEffectiveAccess(int(userID), int(databaseID))
	if err != nil {
		return false, err
	}
	return access >= AccessViewOnly, nil
}

func (s *AccessService) CanEdit(ctx context.Context, userID, databaseID int64) (bool, error) {
	access, err := s.GetEffectiveAccess(int(userID), int(databaseID))
	if err != nil {
		return false, err
	}
	return access == AccessViewEdit, nil
}

func (s *AccessService) AccessibleDatabases(ctx context.Context, userID int64) ([]ports.DatabaseAccess, error) {
	dbs, err := s.repo.GetAllDatabases()
	if err != nil {
		return nil, err
	}

	var result []ports.DatabaseAccess
	for _, db := range dbs {
		access, _ := s.GetEffectiveAccess(int(userID), db.ID)
		if access >= AccessViewOnly {
			level := ports.LevelView
			if access == AccessViewEdit {
				level = ports.LevelEdit
			}
			result = append(result, ports.DatabaseAccess{
				DatabaseID: int64(db.ID),
				Name:       db.Name,
				Level:      level,
			})
		}
	}
	return result, nil
}
