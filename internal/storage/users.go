package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/axllent/mailpit/config"
	"github.com/axllent/mailpit/internal/logger"
	"github.com/axllent/mailpit/internal/shortuuid"
	"github.com/leporo/sqlf"
	"golang.org/x/crypto/bcrypt"
)

const (
	// MinPasswordLength is the minimum length of a user password
	MinPasswordLength = 8

	// maxPasswordLength is the maximum length accepted by bcrypt
	maxPasswordLength = 72
)

var (
	// ErrUserNotFound is returned when a user does not exist
	ErrUserNotFound = errors.New("user not found")

	// ErrInvalidCredentials is returned when the email or password is wrong, or the user is disabled
	ErrInvalidCredentials = errors.New("invalid email or password")

	// ErrLastSuperAdmin is returned when a change would leave no active super admin
	ErrLastSuperAdmin = errors.New("there must be at least one active super admin")

	// dummyPasswordHash is compared against when the user does not exist to keep the response time constant
	dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("mailpit-dummy-password"), bcrypt.DefaultCost)
)

// User is an account which can log in to the web UI.
// Super admins can manage projects & users and access all projects, other users can only
// access the projects assigned to them.
//
// swagger:ignore
type User struct {
	// User ID
	ID string
	// Login email address
	Email string
	// Display name
	Name string
	// Super admins have access to all projects and can manage projects & users
	IsSuperAdmin bool
	// Disabled users cannot log in
	Disabled bool
	// Created date & time
	Created time.Time
	// IDs of the projects the user has been assigned to
	Projects []string
}

// UserUpdate contains the optional changes to a user
type UserUpdate struct {
	Email        *string
	Name         *string
	Password     *string
	IsSuperAdmin *bool
	Disabled     *bool
}

func validatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("the password must be at least %d characters", MinPasswordLength)
	}

	if len(password) > maxPasswordLength {
		return fmt.Errorf("the password cannot be longer than %d characters", maxPasswordLength)
	}

	return nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email {
		return "", errors.New("invalid email address")
	}

	return email, nil
}

// CreateUser creates a user, optionally assigning projects
func CreateUser(email, name, password string, isSuperAdmin bool, projectIDs []string) (*User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}

	if err := validatePassword(password); err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	u := &User{
		ID:           shortuuid.New(),
		Email:        email,
		Name:         strings.TrimSpace(name),
		IsSuperAdmin: isSuperAdmin,
		Created:      time.Now(),
		Projects:     []string{},
	}

	admin := 0
	if u.IsSuperAdmin {
		admin = 1
	}

	_, err = db.Exec(
		fmt.Sprintf(`INSERT INTO %s (ID, Email, Name, PasswordHash, IsSuperAdmin, Disabled, Created) VALUES(?,?,?,?,?,0,?)`, tenant("users")), // #nosec
		u.ID, u.Email, u.Name, string(hash), admin, u.Created.UnixMilli(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("a user with the email \"%s\" already exists", email)
		}

		return nil, err
	}

	if len(projectIDs) > 0 {
		if err := SetUserProjects(u.ID, projectIDs); err != nil {
			_ = DeleteUser(u.ID)
			return nil, err
		}

		u.Projects = projectIDs
	}

	return u, nil
}

// ListUsers returns all users with their assigned projects
func ListUsers() ([]User, error) {
	users := []User{}

	q := sqlf.From(tenant("users")).
		Select("ID, Email, Name, IsSuperAdmin, Disabled, Created").
		OrderBy("Email")

	if err := q.QueryAndClose(context.TODO(), db, func(row *sql.Rows) {
		if u, err := scanUser(row); err != nil {
			logger.Log().Errorf("[db] %s", err.Error())
		} else {
			users = append(users, *u)
		}
	}); err != nil {
		return users, err
	}

	assigned := map[string][]string{}
	pq := sqlf.From(tenant("project_users")).Select("UserID, ProjectID")
	if err := pq.QueryAndClose(context.TODO(), db, func(row *sql.Rows) {
		var userID, projectID string
		if err := row.Scan(&userID, &projectID); err != nil {
			logger.Log().Errorf("[db] %s", err.Error())
			return
		}
		assigned[userID] = append(assigned[userID], projectID)
	}); err != nil {
		return users, err
	}

	for i := range users {
		if ids, ok := assigned[users[i].ID]; ok {
			users[i].Projects = ids
		}
	}

	return users, nil
}

// GetUser returns a user by ID
func GetUser(id string) (*User, error) {
	return getUser("ID", id)
}

// GetUserByEmail returns a user by email address
func GetUserByEmail(email string) (*User, error) {
	return getUser("Email", strings.ToLower(strings.TrimSpace(email)))
}

func getUser(column, value string) (*User, error) {
	var u *User

	q := sqlf.From(tenant("users")).
		Select("ID, Email, Name, IsSuperAdmin, Disabled, Created").
		Where(column+" = ?", value).
		Limit(1)

	if err := q.QueryAndClose(context.TODO(), db, func(row *sql.Rows) {
		var err error
		if u, err = scanUser(row); err != nil {
			logger.Log().Errorf("[db] %s", err.Error())
		}
	}); err != nil {
		return nil, err
	}

	if u == nil {
		return nil, ErrUserNotFound
	}

	ids, err := userProjectIDs(u.ID)
	if err != nil {
		return nil, err
	}
	u.Projects = ids

	return u, nil
}

func scanUser(row *sql.Rows) (*User, error) {
	var (
		u       User
		admin   int
		off     int
		created float64 // use float64 for rqlite compatibility
	)

	if err := row.Scan(&u.ID, &u.Email, &u.Name, &admin, &off, &created); err != nil {
		return nil, err
	}

	u.IsSuperAdmin = admin != 0
	u.Disabled = off != 0
	u.Created = time.UnixMilli(int64(created))
	u.Projects = []string{}

	return &u, nil
}

func userProjectIDs(userID string) ([]string, error) {
	ids := []string{}

	q := sqlf.From(tenant("project_users")).
		Select("ProjectID").
		Where("UserID = ?", userID).
		OrderBy("ProjectID")

	err := q.QueryAndClose(context.TODO(), db, func(row *sql.Rows) {
		var id string
		if err := row.Scan(&id); err != nil {
			logger.Log().Errorf("[db] %s", err.Error())
			return
		}
		ids = append(ids, id)
	})

	return ids, err
}

// activeSuperAdmins returns the number of super admins which are able to log in
func activeSuperAdmins() int {
	var total float64 // use float64 for rqlite compatibility

	_ = sqlf.From(tenant("users")).
		Select("COUNT(*)").To(&total).
		Where("IsSuperAdmin = 1").
		Where("Disabled = 0").
		QueryRowAndClose(context.TODO(), db)

	return int(total)
}

// UpdateUser changes a user. Changing the password, disabling or deleting a user logs the user out.
func UpdateUser(id string, u UserUpdate) (*User, error) {
	user, err := GetUser(id)
	if err != nil {
		return nil, err
	}

	wasActiveAdmin := user.IsSuperAdmin && !user.Disabled

	if u.Email != nil {
		if user.Email, err = normalizeEmail(*u.Email); err != nil {
			return nil, err
		}
	}

	if u.Name != nil {
		user.Name = strings.TrimSpace(*u.Name)
	}

	if u.IsSuperAdmin != nil {
		user.IsSuperAdmin = *u.IsSuperAdmin
	}

	if u.Disabled != nil {
		user.Disabled = *u.Disabled
	}

	if wasActiveAdmin && (!user.IsSuperAdmin || user.Disabled) && activeSuperAdmins() <= 1 {
		return nil, ErrLastSuperAdmin
	}

	admin, disabled := 0, 0
	if user.IsSuperAdmin {
		admin = 1
	}
	if user.Disabled {
		disabled = 1
	}

	if _, err := db.Exec(
		fmt.Sprintf(`UPDATE %s SET Email = ?, Name = ?, IsSuperAdmin = ?, Disabled = ? WHERE ID = ?`, tenant("users")), // #nosec
		user.Email, user.Name, admin, disabled, user.ID,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("a user with the email \"%s\" already exists", user.Email)
		}

		return nil, err
	}

	if u.Password != nil {
		if err := SetUserPassword(user.ID, *u.Password); err != nil {
			return nil, err
		}
	}

	if user.Disabled {
		if err := DeleteUserSessions(user.ID); err != nil {
			return nil, err
		}
	}

	return user, nil
}

// SetUserPassword changes the password of a user and logs the user out of all sessions
func SetUserPassword(id, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	res, err := db.Exec(fmt.Sprintf(`UPDATE %s SET PasswordHash = ? WHERE ID = ?`, tenant("users")), string(hash), id) // #nosec
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrUserNotFound
	}

	return DeleteUserSessions(id)
}

// CheckUserPassword returns whether the password is correct for a user
func CheckUserPassword(id, password string) bool {
	var hash string

	err := sqlf.From(tenant("users")).
		Select("PasswordHash").To(&hash).
		Where("ID = ?", id).
		QueryRowAndClose(context.TODO(), db)
	if err != nil {
		return false
	}

	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// DeleteUser deletes a user and its sessions
func DeleteUser(id string) error {
	user, err := GetUser(id)
	if err != nil {
		return err
	}

	if user.IsSuperAdmin && !user.Disabled && activeSuperAdmins() <= 1 {
		return ErrLastSuperAdmin
	}

	for _, table := range []string{"sessions", "project_users"} {
		column := "UserID"
		if _, err := db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, tenant(table), column), id); err != nil { // #nosec
			return err
		}
	}

	_, err = db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE ID = ?`, tenant("users")), id) // #nosec

	return err
}

// SetUserProjects replaces the projects a user has access to
func SetUserProjects(userID string, projectIDs []string) error {
	if _, err := GetUser(userID); err != nil {
		return err
	}

	unique := []string{}
	seen := map[string]bool{}
	for _, id := range projectIDs {
		if seen[id] {
			continue
		}
		seen[id] = true

		if _, err := GetProject(id); err != nil {
			return fmt.Errorf("project %s not found", id)
		}

		unique = append(unique, id)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE UserID = ?`, tenant("project_users")), userID); err != nil { // #nosec
		return err
	}

	for _, id := range unique {
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s (ProjectID, UserID) VALUES(?, ?)`, tenant("project_users")), id, userID); err != nil { // #nosec
			return err
		}
	}

	return tx.Commit()
}

// UserCanAccessProject returns whether the user is allowed to access the project
func UserCanAccessProject(user *User, projectID string) bool {
	if user == nil || user.Disabled {
		return false
	}

	if user.IsSuperAdmin {
		_, err := GetProject(projectID)

		return err == nil
	}

	for _, id := range user.Projects {
		if id == projectID {
			return true
		}
	}

	return false
}

// UserProjects returns the projects a user has access to. Super admins have access to all projects.
func UserProjects(user *User) ([]Project, error) {
	all, err := ListProjects()
	if err != nil {
		return nil, err
	}

	if user.IsSuperAdmin {
		return all, nil
	}

	projects := []Project{}
	for _, p := range all {
		if UserCanAccessProject(user, p.ID) {
			projects = append(projects, p)
		}
	}

	return projects, nil
}

// AuthenticateUser verifies the email and password of a user
func AuthenticateUser(email, password string) (*User, error) {
	var (
		id, hash string
		disabled int
	)

	email = strings.ToLower(strings.TrimSpace(email))

	err := sqlf.From(tenant("users")).
		Select("ID").To(&id).
		Select("PasswordHash").To(&hash).
		Select("Disabled").To(&disabled).
		Where("Email = ?", email).
		QueryRowAndClose(context.TODO(), db)
	if err != nil {
		// compare anyway so that the response time does not reveal whether the email exists
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))

		return nil, ErrInvalidCredentials
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || disabled != 0 {
		return nil, ErrInvalidCredentials
	}

	return GetUser(id)
}

// EnsureSuperAdmin creates the super admin from the configuration if it does not exist.
// When reset is true the password of an existing user is set to the configured password.
func EnsureSuperAdmin() error {
	if config.SuperAdminEmail == "" {
		n := activeSuperAdmins()
		if n == 0 {
			logger.Log().Warn("[projects] there is no super admin, set MP_SUPERADMIN_EMAIL & MP_SUPERADMIN_PASSWORD to create one")
		}

		return nil
	}

	u, err := GetUserByEmail(config.SuperAdminEmail)
	if errors.Is(err, ErrUserNotFound) {
		if _, err := CreateUser(config.SuperAdminEmail, "Super Admin", config.SuperAdminPassword, true, nil); err != nil {
			return err
		}

		logger.Log().Infof("[projects] created super admin %s", config.SuperAdminEmail)

		return nil
	}

	if err != nil {
		return err
	}

	// make sure the configured user is able to log in as a super admin
	if !u.IsSuperAdmin || u.Disabled {
		admin, active := true, false
		if _, err := UpdateUser(u.ID, UserUpdate{IsSuperAdmin: &admin, Disabled: &active}); err != nil {
			return err
		}
	}

	if config.SuperAdminResetPassword {
		if err := SetUserPassword(u.ID, config.SuperAdminPassword); err != nil {
			return err
		}

		logger.Log().Infof("[projects] reset the password of super admin %s", config.SuperAdminEmail)
	}

	return nil
}

// newToken returns a random session token
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateSession creates a login session for a user and returns the token, which is only stored hashed.
func CreateSession(userID string) (string, time.Time, error) {
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}

	now := time.Now()
	expires := now.Add(time.Duration(config.SessionHours) * time.Hour)

	_, err = db.Exec(
		fmt.Sprintf(`INSERT INTO %s (TokenHash, UserID, Created, Expires) VALUES(?,?,?,?)`, tenant("sessions")), // #nosec
		hashSecret(token), userID, now.UnixMilli(), expires.UnixMilli(),
	)
	if err != nil {
		return "", time.Time{}, err
	}

	return token, expires, nil
}

// SessionUser returns the user of a valid, unexpired session token
func SessionUser(token string) (*User, error) {
	if token == "" || len(token) > 128 {
		return nil, ErrUserNotFound
	}

	var (
		userID  string
		expires float64 // use float64 for rqlite compatibility
	)

	err := sqlf.From(tenant("sessions")).
		Select("UserID").To(&userID).
		Select("Expires").To(&expires).
		Where("TokenHash = ?", hashSecret(token)).
		QueryRowAndClose(context.TODO(), db)
	if err != nil {
		return nil, ErrUserNotFound
	}

	if time.Now().UnixMilli() > int64(expires) {
		_ = DeleteSession(token)

		return nil, ErrUserNotFound
	}

	u, err := GetUser(userID)
	if err != nil {
		return nil, err
	}

	if u.Disabled {
		return nil, ErrUserNotFound
	}

	return u, nil
}

// DeleteSession logs out a session
func DeleteSession(token string) error {
	_, err := db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE TokenHash = ?`, tenant("sessions")), hashSecret(token)) // #nosec

	return err
}

// DeleteUserSessions logs a user out of all sessions
func DeleteUserSessions(userID string) error {
	_, err := db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE UserID = ?`, tenant("sessions")), userID) // #nosec

	return err
}

// purgeExpiredSessions deletes all expired sessions
func purgeExpiredSessions() {
	if _, err := db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE Expires < ?`, tenant("sessions")), time.Now().UnixMilli()); err != nil { // #nosec
		logger.Log().Errorf("[db] %s", err.Error())
	}
}
