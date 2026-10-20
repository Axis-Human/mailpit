package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/axllent/mailpit/internal/logger"
	"github.com/axllent/mailpit/internal/shortuuid"
	"github.com/leporo/sqlf"
)

const (
	// apiKeyPrefix is added to the start of every generated project API key
	apiKeyPrefix = "mpk_"

	// projectCacheTTL is how long a project API key lookup is cached. All project changes made by this
	// process invalidate the cache immediately, the TTL only matters when sharing a database (rqlite).
	projectCacheTTL = 5 * time.Second
)

var (
	// ErrProjectNotFound is returned when a project does not exist
	ErrProjectNotFound = errors.New("project not found")

	// projectSlugRe matches a valid project slug
	projectSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

	// slugCleanRe matches the characters removed when generating a slug from a name
	slugCleanRe = regexp.MustCompile(`[^a-z0-9]+`)

	projectCache      = map[string]cachedProject{}
	projectCacheMutex sync.Mutex
)

// Project is an isolated mailbox with its own API key
//
// swagger:ignore
type Project struct {
	// Project ID
	ID string
	// Short unique name used in URLs and as the suggested SMTP username
	Slug string
	// Display name
	Name string
	// The last characters of the API key, to help identify it. The key itself is never stored.
	KeyHint string
	// Disabled projects reject their API key and all incoming messages
	Disabled bool
	// Created date & time
	Created time.Time
	// Total messages (only set when listing projects)
	Messages uint64 `json:",omitempty"`
	// Unread messages (only set when listing projects)
	Unread uint64 `json:",omitempty"`
}

type cachedProject struct {
	project *Project
	expires time.Time
}

// ProjectUpdate contains the optional changes to a project
type ProjectUpdate struct {
	Name     *string
	Slug     *string
	Disabled *bool
}

// NewProjectSlug returns a valid slug generated from a name, or an empty string if not possible
func NewProjectSlug(name string) string {
	slug := strings.Trim(slugCleanRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 63 {
		slug = strings.Trim(slug[:63], "-")
	}

	return slug
}

// ValidProjectSlug returns whether the slug is valid
func ValidProjectSlug(slug string) bool {
	return projectSlugRe.MatchString(slug)
}

// generateAPIKey returns a new random API key and its hash
func generateAPIKey() (key, hash, hint string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", err
	}

	key = apiKeyPrefix + base64.RawURLEncoding.EncodeToString(b)

	return key, hashSecret(key), key[len(key)-4:], nil
}

// hashSecret returns the hex SHA-256 hash of a high entropy secret (API keys & session tokens)
func hashSecret(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

func clearProjectCache() {
	projectCacheMutex.Lock()
	projectCache = map[string]cachedProject{}
	projectCacheMutex.Unlock()
}

// CreateProject creates a new project and returns it with the generated API key.
// The API key cannot be retrieved afterwards, only rotated.
// If the slug is blank then one is generated from the name.
func CreateProject(name, slug string) (*Project, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", errors.New("project name is required")
	}

	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		slug = NewProjectSlug(name)
	}

	if !ValidProjectSlug(slug) {
		return nil, "", errors.New("invalid project slug, it can only contain lowercase letters, numbers and hyphens")
	}

	key, hash, hint, err := generateAPIKey()
	if err != nil {
		return nil, "", err
	}

	p := &Project{
		ID:      shortuuid.New(),
		Slug:    slug,
		Name:    name,
		KeyHint: hint,
		Created: time.Now(),
	}

	_, err = db.Exec(
		fmt.Sprintf(`INSERT INTO %s (ID, Slug, Name, KeyHash, KeyHint, Disabled, Created) VALUES(?,?,?,?,?,0,?)`, tenant("projects")), // #nosec
		p.ID, p.Slug, p.Name, hash, hint, p.Created.UnixMilli(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, "", fmt.Errorf("a project with the slug \"%s\" already exists", slug)
		}

		return nil, "", err
	}

	clearProjectCache()

	return p, key, nil
}

// ListProjects returns all projects, including their message totals
func ListProjects() ([]Project, error) {
	projects := []Project{}

	q := sqlf.From(tenant("projects")).
		Select("ID, Slug, Name, KeyHint, Disabled, Created").
		OrderBy("Name COLLATE NOCASE")

	if err := q.QueryAndClose(context.TODO(), db, func(row *sql.Rows) {
		if p, err := scanProject(row); err != nil {
			logger.Log().Errorf("[db] %s", err.Error())
		} else {
			projects = append(projects, *p)
		}
	}); err != nil {
		return projects, err
	}

	for i := range projects {
		projects[i].Messages = CountTotal(projects[i].ID)
		projects[i].Unread = CountUnread(projects[i].ID)
	}

	return projects, nil
}

// GetProject returns a project by ID
func GetProject(id string) (*Project, error) {
	return getProject("ID", id)
}

// GetProjectBySlug returns a project by its slug
func GetProjectBySlug(slug string) (*Project, error) {
	return getProject("Slug", strings.ToLower(slug))
}

// ProjectByAPIKey returns the project (enabled or disabled) which uses the API key.
func ProjectByAPIKey(key string) (*Project, error) {
	if !strings.HasPrefix(key, apiKeyPrefix) || len(key) > 128 {
		return nil, ErrProjectNotFound
	}

	hash := hashSecret(key)

	projectCacheMutex.Lock()
	cached, ok := projectCache[hash]
	projectCacheMutex.Unlock()

	if ok && time.Now().Before(cached.expires) {
		if cached.project == nil {
			return nil, ErrProjectNotFound
		}

		p := *cached.project

		return &p, nil
	}

	p, err := getProject("KeyHash", hash)
	if err != nil && !errors.Is(err, ErrProjectNotFound) {
		return nil, err
	}

	projectCacheMutex.Lock()
	// do not let invalid keys grow the cache without limit
	if len(projectCache) > 1000 {
		projectCache = map[string]cachedProject{}
	}
	projectCache[hash] = cachedProject{project: p, expires: time.Now().Add(projectCacheTTL)}
	projectCacheMutex.Unlock()

	return p, err
}

// ProjectEnabled returns whether a project exists and has not been disabled
func ProjectEnabled(id string) bool {
	p, err := GetProject(id)

	return err == nil && !p.Disabled
}

func getProject(column, value string) (*Project, error) {
	var p *Project

	q := sqlf.From(tenant("projects")).
		Select("ID, Slug, Name, KeyHint, Disabled, Created").
		Where(column+" = ?", value).
		Limit(1)

	if err := q.QueryAndClose(context.TODO(), db, func(row *sql.Rows) {
		var err error
		if p, err = scanProject(row); err != nil {
			logger.Log().Errorf("[db] %s", err.Error())
		}
	}); err != nil {
		return nil, err
	}

	if p == nil {
		return nil, ErrProjectNotFound
	}

	return p, nil
}

func scanProject(row *sql.Rows) (*Project, error) {
	var (
		p        Project
		disabled int
		created  float64 // use float64 for rqlite compatibility
	)

	if err := row.Scan(&p.ID, &p.Slug, &p.Name, &p.KeyHint, &disabled, &created); err != nil {
		return nil, err
	}

	p.Disabled = disabled != 0
	p.Created = time.UnixMilli(int64(created))

	return &p, nil
}

// UpdateProject changes a project name, slug or whether it is disabled
func UpdateProject(id string, u ProjectUpdate) (*Project, error) {
	p, err := GetProject(id)
	if err != nil {
		return nil, err
	}

	if u.Name != nil {
		name := strings.TrimSpace(*u.Name)
		if name == "" {
			return nil, errors.New("project name is required")
		}
		p.Name = name
	}

	if u.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*u.Slug))
		if !ValidProjectSlug(slug) {
			return nil, errors.New("invalid project slug, it can only contain lowercase letters, numbers and hyphens")
		}
		p.Slug = slug
	}

	if u.Disabled != nil {
		p.Disabled = *u.Disabled
	}

	disabled := 0
	if p.Disabled {
		disabled = 1
	}

	_, err = db.Exec(
		fmt.Sprintf(`UPDATE %s SET Name = ?, Slug = ?, Disabled = ? WHERE ID = ?`, tenant("projects")), // #nosec
		p.Name, p.Slug, disabled, p.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("a project with the slug \"%s\" already exists", p.Slug)
		}

		return nil, err
	}

	clearProjectCache()

	if p.Disabled {
		logger.Log().Infof("[projects] disabled project %s (%s)", p.Slug, p.ID)
	}

	return p, nil
}

// RotateProjectAPIKey generates a new API key for a project, immediately invalidating the previous one.
func RotateProjectAPIKey(id string) (string, error) {
	key, hash, hint, err := generateAPIKey()
	if err != nil {
		return "", err
	}

	res, err := db.Exec(
		fmt.Sprintf(`UPDATE %s SET KeyHash = ?, KeyHint = ? WHERE ID = ?`, tenant("projects")), // #nosec
		hash, hint, id,
	)
	if err != nil {
		return "", err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return "", ErrProjectNotFound
	}

	clearProjectCache()

	return key, nil
}

// DeleteProject deletes a project including all of its messages
func DeleteProject(id string) error {
	if _, err := GetProject(id); err != nil {
		return err
	}

	if err := DeleteAllMessages(id); err != nil {
		return err
	}

	for _, query := range []string{
		fmt.Sprintf(`DELETE FROM %s WHERE ProjectID = ?`, tenant("project_users")),
		fmt.Sprintf(`DELETE FROM %s WHERE ID = ?`, tenant("projects")),
	} {
		if _, err := db.Exec(query, id); err != nil { // #nosec
			return err
		}
	}

	clearProjectCache()

	return nil
}

// isUniqueViolation returns whether the error is caused by a unique index
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
