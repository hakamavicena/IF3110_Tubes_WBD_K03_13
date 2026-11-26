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

// Ini adalah inti logikamu. Fungsi ini mencari semua grup yang dimiliki seorang user, 
// lalu mencari level akses tertinggi (0 = None, 1 = View, 2 = Edit) 
// dari grup-grup tersebut terhadap suatu database.
func (s *AccessService) GetEffectiveAccess(userID, dbID int) (AccessLevel, error) {
	//ngecek userID nya ada atau ga
	_, err := s.repo.GetUser(userID)
	if err != nil {
		return AccessNone, err
	}
	//ngelihat semua grup milik user
	groups, err := s.repo.GetGroupsByUserID(userID)
	if err != nil {
		return AccessNone, err
	}

	effectiveAccess := AccessNone
	//baru meriksa akses dari setiap grup
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


//dead code
func (s *AccessService) CanExecuteMigration(userID, dbID int) (bool, error) {
	access, err := s.GetEffectiveAccess(userID, dbID)
	if err != nil {
		return false, err
	}
	return access == AccessViewEdit, nil
}

//dead code
func (s *AccessService) CanViewSchema(userID, dbID int) (bool, error) {
	access, err := s.GetEffectiveAccess(userID, dbID)
	if err != nil {
		return false, err
	}
	return access >= AccessViewOnly, nil
}
