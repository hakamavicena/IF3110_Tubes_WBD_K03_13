package accesscontrol

import (
	"database/sql"
	"errors"
)

type AccessService struct {
	repo AccessRepository
}

func NewAccessService(repo AccessRepository) *AccessService {
	return &AccessService{repo: repo}
}

func (s *AccessService) GetEffectiveAccess(userID, dbID int) (AccessLevel, error) {
	_, err := s.repo.GetUser(userID)
	if err != nil {
		return AccessNone, err
	}

	groups, err := s.repo.GetGroupsByUserID(userID)
	if err != nil {
		return AccessNone, err
	}

	effectiveAccess := AccessNone
	for _, group := range groups {
		level, err := s.repo.GetAccessByGroupAndDB(group.ID, dbID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound) {
				continue
			}
			return AccessNone, err
		}
		if level > effectiveAccess {
			effectiveAccess = level
		}
	}

	return effectiveAccess, nil
}

// cek user adalah admin?
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

func (s *AccessService) CanExecuteMigration(userID, dbID int) (bool, error) {
	access, err := s.GetEffectiveAccess(userID, dbID)
	if err != nil {
		return false, err
	}
	return access == AccessViewEdit, nil
}

func (s *AccessService) CanViewSchema(userID, dbID int) (bool, error) {
	access, err := s.GetEffectiveAccess(userID, dbID)
	if err != nil {
		return false, err
	}
	return access >= AccessViewOnly, nil
}
