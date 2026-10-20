package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/axllent/mailpit/config"
	"github.com/axllent/mailpit/internal/logger"
	"github.com/axllent/mailpit/internal/storage"
	"github.com/axllent/mailpit/internal/tenancy"
)

const (
	// SessionCookie is the name of the cookie which holds the login session token
	SessionCookie = "mailpit_session"

	// projectHeader is the request header which selects the project for users who are logged in
	projectHeader = "X-Mailpit-Project"

	// apiKeyHeader is the request header which carries a project API key
	apiKeyHeader = "X-API-Key"
)

// accessLevel is the authentication a route requires in multi-project mode
type accessLevel int

const (
	// accessProject requires a login or API key, and a project to apply the request to
	accessProject accessLevel = iota
	// accessAny requires a login or API key, but not a project
	accessAny
	// accessAdmin requires a login of a super admin
	accessAdmin
	// accessPublic does not require authentication
	accessPublic
)

// accessError is returned when a request is not allowed
type accessError struct {
	status  int
	message string
}

func (e *accessError) Error() string {
	return e.message
}

// writeAccessError writes a JSON error response
func writeAccessError(w http.ResponseWriter, err *accessError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.status)
	_ = json.NewEncoder(w).Encode(struct{ Error string }{Error: err.message})
}

// requireAccess authenticates a request in multi-project mode and returns the request
// with the principal set. It writes the error response and returns false when access is denied.
// It does nothing in single mailbox mode.
func requireAccess(w http.ResponseWriter, r *http.Request, level accessLevel) (*http.Request, bool) {
	if !config.MultiProject {
		return r, true
	}

	p, err := authenticate(r)

	if level == accessPublic {
		// public routes only identify the caller when possible
		if err == nil {
			r = tenancy.WithPrincipal(r, p)
		}

		return r, true
	}

	if err != nil {
		writeAccessError(w, err)
		return r, false
	}

	switch level {
	case accessAdmin:
		if p.ViaAPIKey || !p.SuperAdmin {
			writeAccessError(w, &accessError{http.StatusForbidden, "super admin access is required"})
			return r, false
		}
	case accessProject:
		if err := resolveProject(r, p); err != nil {
			writeAccessError(w, err)
			return r, false
		}
	}

	return tenancy.WithPrincipal(r, p), true
}

// authenticate identifies the caller using a project API key or the login session cookie
func authenticate(r *http.Request) (*tenancy.Principal, *accessError) {
	if key := apiKeyFromRequest(r); key != "" {
		project, err := storage.ProjectByAPIKey(key)
		if err != nil {
			return nil, &accessError{http.StatusUnauthorized, "invalid API key"}
		}

		if project.Disabled {
			return nil, &accessError{http.StatusForbidden, "this project has been disabled"}
		}

		return &tenancy.Principal{ViaAPIKey: true, ProjectID: project.ID}, nil
	}

	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, &accessError{http.StatusUnauthorized, "authentication required"}
	}

	user, err := storage.SessionUser(cookie.Value)
	if err != nil {
		if !errors.Is(err, storage.ErrUserNotFound) {
			logger.Log().Errorf("[auth] %s", err.Error())
		}

		return nil, &accessError{http.StatusUnauthorized, "authentication required"}
	}

	return &tenancy.Principal{
		UserID:     user.ID,
		Email:      user.Email,
		SuperAdmin: user.IsSuperAdmin,
		Projects:   user.Projects,
	}, nil
}

// apiKeyFromRequest returns the API key sent with the request, if any. It can be set using the
// X-API-Key header, a Bearer token, or as the password of Basic authentication.
func apiKeyFromRequest(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get(apiKeyHeader)); key != "" {
		return key
	}

	header := r.Header.Get("Authorization")
	if len(header) > 7 && strings.EqualFold(header[:7], "bearer ") {
		return strings.TrimSpace(header[7:])
	}

	if _, pass, ok := r.BasicAuth(); ok {
		return pass
	}

	return ""
}

// resolveProject sets the project of a request. API keys are bound to a project, for users it is
// selected using the project header or query argument, or taken from the message in the URL.
func resolveProject(r *http.Request, p *tenancy.Principal) *accessError {
	if p.ViaAPIKey {
		return nil
	}

	selected := strings.TrimSpace(r.Header.Get(projectHeader))
	if selected == "" {
		selected = strings.TrimSpace(r.URL.Query().Get("project"))
	}

	var projectID string

	if selected != "" {
		project, err := storage.GetProjectBySlug(selected)
		if err != nil {
			project, err = storage.GetProject(selected)
		}

		if err != nil || !p.CanAccess(project.ID) {
			return &accessError{http.StatusForbidden, "you do not have access to this project"}
		}

		projectID = project.ID
	}

	// links to messages (attachments, raw, html previews) do not carry the header, so for these
	// requests the project is the one of the message.
	if id := messageIDFromRequest(r); id != "" && id != "latest" {
		messageProject, ok := storage.MessageProjectID(id)
		if !ok || !p.CanAccess(messageProject) || (projectID != "" && projectID != messageProject) {
			return &accessError{http.StatusNotFound, "message not found"}
		}

		projectID = messageProject
	}

	if projectID == "" {
		return &accessError{http.StatusBadRequest, "no project selected"}
	}

	p.ProjectID = projectID

	return nil
}

// messageIDFromRequest returns the ID of the message the request is for, if any
func messageIDFromRequest(r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}

	if path, ok := strings.CutPrefix(r.URL.Path, config.Webroot+"view/"); ok {
		path = strings.TrimSuffix(strings.TrimSuffix(path, ".html"), ".txt")
		if path != "" && !strings.Contains(path, "/") {
			return path
		}
	}

	return ""
}
