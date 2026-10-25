package accesscontrol

// kontrak nyimpan dn ngambil data relasi akses dan entitas terkait.
type AccessRepository interface {
	GetUser(userID int) (*User, error)
	GetAllUsers() ([]User, error)
	CreateUser(user *User) error

	GetGroup(groupID int) (*Group, error)
	GetAllGroups() ([]Group, error)
	CreateGroup(name string) (*Group, error)

	GetGroupsByUserID(userID int) ([]Group, error)
	AddUserToGroup(userID, groupID int) error
	RemoveUserFromGroup(userID, groupID int) error

	GetDatabase(dbID int) (*Database, error)
	GetAllDatabases() ([]Database, error)
	CreateDatabase(db *Database) error

	SetGroupAccess(groupID, databaseID int, level AccessLevel) error
	GetAccessByGroupAndDB(groupID, databaseID int) (AccessLevel, error)
	GetAccessibleDatabases(userID int) ([]DatabaseWithAccess, error)
}
