package accesscontrol

import (
	"database/sql"
	_ "github.com/lib/pq"
)

type postgresRepository struct {
	db *sql.DB
}

// buat instance baru dari repository PostgreSQL
func NewPostgresRepository(db *sql.DB) AccessRepository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) GetUser(userID int) (*User, error) {
	u := &User{}
	err := r.db.QueryRow("SELECT id, username, is_admin FROM users WHERE id = $1", userID).Scan(&u.ID, &u.Username, &u.IsAdmin)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return u, err
}

func (r *postgresRepository) GetAllUsers() ([]User, error) {
	rows, err := r.db.Query("SELECT id, username, is_admin FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (r *postgresRepository) CreateUser(user *User) error {
	return r.db.QueryRow("INSERT INTO users (username, is_admin) VALUES ($1, $2) RETURNING id", user.Username, user.IsAdmin).Scan(&user.ID)
}

func (r *postgresRepository) GetGroup(groupID int) (*Group, error) {
	g := &Group{}
	err := r.db.QueryRow("SELECT id, name FROM groups WHERE id = $1", groupID).Scan(&g.ID, &g.Name)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return g, err
}

func (r *postgresRepository) GetAllGroups() ([]Group, error) {
	rows, err := r.db.Query("SELECT id, name FROM groups")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func (r *postgresRepository) CreateGroup(name string) (*Group, error) {
	g := &Group{Name: name}
	err := r.db.QueryRow("INSERT INTO groups (name) VALUES ($1) RETURNING id", name).Scan(&g.ID)
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (r *postgresRepository) GetGroupsByUserID(userID int) ([]Group, error) {
	rows, err := r.db.Query(`
		SELECT g.id, g.name 
		FROM groups g
		JOIN user_groups ug ON g.id = ug.group_id
		WHERE ug.user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func (r *postgresRepository) AddUserToGroup(userID, groupID int) error {
	_, err := r.db.Exec("INSERT INTO user_groups (user_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", userID, groupID)
	return err
}

func (r *postgresRepository) RemoveUserFromGroup(userID, groupID int) error {
	_, err := r.db.Exec("DELETE FROM user_groups WHERE user_id = $1 AND group_id = $2", userID, groupID)
	return err
}

func (r *postgresRepository) GetDatabase(dbID int) (*Database, error) {
	d := &Database{}
	err := r.db.QueryRow(`
		SELECT id, name, host, port, database_name, username, password_enc, ssl_mode 
		FROM database_targets WHERE id = $1
	`, dbID).Scan(&d.ID, &d.Name, &d.Host, &d.Port, &d.DatabaseName, &d.Username, &d.PasswordEnc, &d.SSLMode)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return d, err
}

func (r *postgresRepository) GetAllDatabases() ([]Database, error) {
	rows, err := r.db.Query(`
		SELECT id, name, host, port, database_name, username, password_enc, ssl_mode 
		FROM database_targets 
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.ID, &d.Name, &d.Host, &d.Port, &d.DatabaseName, &d.Username, &d.PasswordEnc, &d.SSLMode); err != nil {
			return nil, err
		}
		dbs = append(dbs, d)
	}
	return dbs, nil
}

func (r *postgresRepository) CreateDatabase(db *Database) error {
	sslMode := db.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}
	return r.db.QueryRow(`
		INSERT INTO database_targets (name, host, port, database_name, username, password_enc, ssl_mode) 
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id
	`, db.Name, db.Host, db.Port, db.DatabaseName, db.Username, db.PasswordEnc, sslMode).Scan(&db.ID)
}

func (r *postgresRepository) SetGroupAccess(groupID, databaseID int, level AccessLevel) error {
	_, err := r.db.Exec(`
		INSERT INTO group_database_access (group_id, database_id, access_level) 
		VALUES ($1, $2, $3)
		ON CONFLICT (group_id, database_id) 
		DO UPDATE SET access_level = EXCLUDED.access_level
	`, groupID, databaseID, int(level))
	return err
}

func (r *postgresRepository) GetAccessByGroupAndDB(groupID, databaseID int) (AccessLevel, error) {
	var level int
	err := r.db.QueryRow(`
		SELECT access_level 
		FROM group_database_access 
		WHERE group_id = $1 AND database_id = $2
	`, groupID, databaseID).Scan(&level)
	if err == sql.ErrNoRows {
		return AccessNone, nil
	}
	return AccessLevel(level), err
}

func (r *postgresRepository) GetAccessibleDatabases(userID int) ([]DatabaseWithAccess, error) {
	rows, err := r.db.Query(`
		SELECT dt.id, dt.name, MAX(gda.access_level) as max_level
		FROM database_targets dt
		JOIN group_database_access gda ON dt.id = gda.database_id
		JOIN user_groups ug ON gda.group_id = ug.group_id
		WHERE ug.user_id = $1
		GROUP BY dt.id, dt.name
		HAVING MAX(gda.access_level) > 0
		ORDER BY dt.name ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DatabaseWithAccess
	for rows.Next() {
		var item DatabaseWithAccess
		var lvl int
		if err := rows.Scan(&item.DatabaseID, &item.Name, &lvl); err != nil {
			return nil, err
		}
		item.Level = AccessLevel(lvl)
		result = append(result, item)
	}
	return result, nil
}
