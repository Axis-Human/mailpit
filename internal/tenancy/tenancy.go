// Package tenancy carries the authenticated project & user of a request in multi-project mode.
//
// In single mailbox mode (the default) there is no principal, and the project is always "".
package tenancy

import (
	"context"
	"net/http"

	"github.com/axllent/mailpit/config"
)

type contextKey int

const principalKey contextKey = iota

// Principal is the authenticated caller of a request
type Principal struct {
	// UserID is set when the caller logged in with a user account
	UserID string
	// Email of the user
	Email string
	// SuperAdmin is true for users who manage projects & users
	SuperAdmin bool
	// ViaAPIKey is true when the caller authenticated with a project API key
	ViaAPIKey bool
	// ProjectID is the project all message operations apply to
	ProjectID string
	// Projects are the IDs of the projects a user (who is not a super admin) has been assigned to
	Projects []string
}

// CanAccess returns whether the principal is allowed to access the messages of a project
func (p *Principal) CanAccess(projectID string) bool {
	switch {
	case p == nil:
		return !config.MultiProject
	case p.ViaAPIKey:
		return p.ProjectID == projectID
	case p.SuperAdmin:
		return true
	}

	for _, id := range p.Projects {
		if id == projectID {
			return true
		}
	}

	return false
}

// WithPrincipal returns a copy of the request which carries the principal
func WithPrincipal(r *http.Request, p *Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey, p))
}

// FromRequest returns the principal of a request, or nil if there is none
func FromRequest(r *http.Request) *Principal {
	p, _ := r.Context().Value(principalKey).(*Principal)

	return p
}

// ProjectID returns the project of the request. It is "" in single mailbox mode.
func ProjectID(r *http.Request) string {
	if p := FromRequest(r); p != nil {
		return p.ProjectID
	}

	return ""
}

// IsSuperAdmin returns whether the request may change global settings. This is always true in
// single mailbox mode, as there are no users.
func IsSuperAdmin(r *http.Request) bool {
	if p := FromRequest(r); p != nil {
		return p.SuperAdmin
	}

	return !config.MultiProject
}
