package accesscontrol

import (
	"context"
	"database/sql"
	"errors"
)

type pgApprovalStore struct{ db *sql.DB }

func NewPostgresApprovalStore(db *sql.DB) ApprovalStore { return &pgApprovalStore{db: db} }

type pgQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

const selectJob = `SELECT id, database_id, migration_id, action, status, requested_by, created_at
                   FROM migration_jobs WHERE id = $1 AND database_id = $2`

func scanJob(row *sql.Row) (*Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.DatabaseID, &j.MigrationID, &j.Action, &j.Status, &j.RequestedBy, &j.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

const selectRequired = `
	SELECT g.id, g.name, gda.access_level, g.leader_id, COALESCE(lu.username, ''),
	       COALESCE(lu.is_active, false), (ma.job_id IS NOT NULL), ma.approved_at
	FROM group_database_access gda
	JOIN groups g ON g.id = gda.group_id
	LEFT JOIN users lu ON lu.id = g.leader_id
	LEFT JOIN migration_approvals ma ON ma.group_id = g.id AND ma.job_id = $1
	WHERE gda.database_id = $2 AND gda.access_level > 0
	ORDER BY g.name ASC, g.id ASC`

func queryRequired(ctx context.Context, q pgQuerier, jobID, databaseID int64) ([]RequiredApproval, error) {
	rows, err := q.QueryContext(ctx, selectRequired, jobID, databaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RequiredApproval{}
	for rows.Next() {
		var r RequiredApproval
		var leader sql.NullInt64
		var at sql.NullTime
		if err := rows.Scan(&r.GroupID, &r.GroupName, &r.AccessLevel, &leader, &r.LeaderUsername,
			&r.LeaderActive, &r.Approved, &at); err != nil {
			return nil, err
		}
		if leader.Valid {
			v := int(leader.Int64)
			r.LeaderID = &v
		}
		if at.Valid {
			t := at.Time
			r.ApprovedAt = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *pgApprovalStore) ReadJob(ctx context.Context, databaseID, jobID int64) (*Job, []RequiredApproval, error) {
	job, err := scanJob(s.db.QueryRowContext(ctx, selectJob, jobID, databaseID))
	if err != nil {
		return nil, nil, err
	}
	req, err := queryRequired(ctx, s.db, jobID, databaseID)
	return job, req, err
}

type pgApprovalTx struct {
	tx         *sql.Tx
	databaseID int64
	jobID      int64
}

func (s *pgApprovalStore) WithJobLock(ctx context.Context, databaseID, jobID int64, fn func(tx ApprovalTx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var locked int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM migration_jobs WHERE id = $1 AND database_id = $2 FOR UPDATE`,
		jobID, databaseID).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrJobNotFound
	}
	if err != nil {
		return err
	}
	if err := fn(&pgApprovalTx{tx: tx, databaseID: databaseID, jobID: jobID}); err != nil {
		return err
	}
	return tx.Commit()
}

func (t *pgApprovalTx) GetJob(ctx context.Context) (*Job, error) {
	return scanJob(t.tx.QueryRowContext(ctx, selectJob, t.jobID, t.databaseID))
}

func (t *pgApprovalTx) Required(ctx context.Context) ([]RequiredApproval, error) {
	return queryRequired(ctx, t.tx, t.jobID, t.databaseID)
}

func (t *pgApprovalTx) InsertApproval(ctx context.Context, groupID, leaderID int) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO migration_approvals (job_id, group_id, leader_id) VALUES ($1, $2, $3)
		ON CONFLICT (job_id, group_id) DO NOTHING`, t.jobID, groupID, leaderID)
	return err
}

func (t *pgApprovalTx) SetStatus(ctx context.Context, from, to string) (bool, error) {
	res, err := t.tx.ExecContext(ctx, `UPDATE migration_jobs SET status = $3 WHERE id = $1 AND status = $2`, t.jobID, from, to)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		_, _ = t.tx.ExecContext(ctx, `UPDATE migrations SET status = $2 WHERE id = (SELECT migration_id FROM migration_jobs WHERE id = $1)`, t.jobID, to)
	}
	return n == 1, nil
}

func (s *pgApprovalStore) PendingForLeader(ctx context.Context, userID int64) ([]InboxItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT j.id, j.database_id, dt.name, j.action, m.version, m.title, u.username, j.created_at, g.id, g.name
		FROM migration_jobs j
		JOIN database_targets dt ON dt.id = j.database_id
		JOIN migrations m ON m.id = j.migration_id
		JOIN users u ON u.id = j.requested_by
		JOIN group_database_access gda ON gda.database_id = j.database_id AND gda.access_level > 0
		JOIN groups g ON g.id = gda.group_id AND g.leader_id = $1
		LEFT JOIN migration_approvals ma ON ma.job_id = j.id AND ma.group_id = g.id
		WHERE j.status = 'pending_review' AND ma.job_id IS NULL
		ORDER BY j.created_at ASC, j.id ASC, g.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []InboxItem{}
	idx := map[int64]int{}
	for rows.Next() {
		var it InboxItem
		var g LeaderGroup
		if err := rows.Scan(&it.JobID, &it.DatabaseID, &it.DatabaseName, &it.Action, &it.Version, &it.Title,
			&it.RequestedBy, &it.CreatedAt, &g.ID, &g.Name); err != nil {
			return nil, err
		}
		if i, ok := idx[it.JobID]; ok {
			items[i].Groups = append(items[i].Groups, g)
			continue
		}
		it.Groups = []LeaderGroup{g}
		idx[it.JobID] = len(items)
		items = append(items, it)
	}
	return items, rows.Err()
}
