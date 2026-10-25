package accesscontrol

import (
	"context"

	"backend/dbengine/pkg/ports"
)

var _ ports.Authorizer = (*Authorizer)(nil)

type Authorizer struct {
	service *AccessService
	repo    AccessRepository
}

func NewAuthorizer(service *AccessService, repo AccessRepository) *Authorizer {
	return &Authorizer{service: service, repo: repo}
}

func toPortsLevel(lvl AccessLevel) ports.Level {
	switch lvl {
	case AccessViewOnly:
		return ports.LevelView
	case AccessViewEdit:
		return ports.LevelEdit
	default:
		return ports.LevelNone
	}
}

func (a *Authorizer) CanView(ctx context.Context, userID, databaseID int64) (bool, error) {
	level, err := a.service.GetEffectiveAccess(int(userID), int(databaseID))
	if err != nil {
		return false, err
	}
	return level >= AccessViewOnly, nil
}

func (a *Authorizer) CanEdit(ctx context.Context, userID, databaseID int64) (bool, error) {
	level, err := a.service.GetEffectiveAccess(int(userID), int(databaseID))
	if err != nil {
		return false, err
	}
	return level >= AccessViewEdit, nil
}

func (a *Authorizer) AccessibleDatabases(ctx context.Context, userID int64) ([]ports.DatabaseAccess, error) {
	dbs, err := a.repo.GetAccessibleDatabases(int(userID))
	if err != nil {
		return nil, err
	}

	result := make([]ports.DatabaseAccess, 0, len(dbs))
	for _, db := range dbs {
		if db.Level > AccessNone {
			result = append(result, ports.DatabaseAccess{
				DatabaseID: db.DatabaseID,
				Name:       db.Name,
				Level:      toPortsLevel(db.Level),
			})
		}
	}
	return result, nil
}
