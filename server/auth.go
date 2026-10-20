package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/axllent/mailpit/config"
	"github.com/axllent/mailpit/internal/logger"
	"github.com/axllent/mailpit/internal/storage"
	"github.com/axllent/mailpit/internal/tenancy"
)

// authUser is the information about a logged in user returned to the web UI
type authUser struct {
	ID           string
	Email        string
	Name         string
	IsSuperAdmin bool
}

// authStatus is the response of the status & login endpoints
type authStatus struct {
	// Whether projects & users are enabled, the web UI requires a login if so
	MultiProject bool
	// The logged in user, null if not logged in
	User *authUser
	// The projects the user has access to
	Projects []storage.Project
}

// jsonResponse writes a JSON response
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Log().Errorf("[http] %s", err.Error())
	}
}

// jsonError writes a JSON error response
func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, struct{ Error string }{Error: msg})
}

// decodeJSON decodes the request body, writing an error response if it is invalid
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return false
	}

	return true
}

// currentUser returns the logged in user of a request
func currentUser(r *http.Request) (*storage.User, error) {
	p := tenancy.FromRequest(r)
	if p == nil || p.ViaAPIKey || p.UserID == "" {
		return nil, storage.ErrUserNotFound
	}

	return storage.GetUser(p.UserID)
}

// statusFor returns the auth status for the user of a request
func statusFor(r *http.Request) authStatus {
	status := authStatus{MultiProject: config.MultiProject, Projects: []storage.Project{}}

	if !config.MultiProject {
		return status
	}

	user, err := currentUser(r)
	if err != nil {
		return status
	}

	status.User = &authUser{ID: user.ID, Email: user.Email, Name: user.Name, IsSuperAdmin: user.IsSuperAdmin}

	if projects, err := storage.UserProjects(user); err != nil {
		logger.Log().Errorf("[auth] %s", err.Error())
	} else {
		status.Projects = projects
	}

	return status
}

// authStatusHandler returns whether the web UI requires a login, and the current user & projects
func authStatusHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, statusFor(r))
}

// clientIP returns the address of the client connection
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// isHTTPS returns whether the client connection is secure, including behind a TLS terminating proxy
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     config.Webroot,
		Expires:  expires,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		// the cookie is never sent with cross-site requests, which protects against CSRF
		SameSite: http.SameSiteStrictMode,
	})
}

// loginHandler logs a user in
func loginHandler(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Email    string
		Password string
	}

	if !decodeJSON(w, r, &data) {
		return
	}

	email := strings.ToLower(strings.TrimSpace(data.Email))
	ip := clientIP(r)

	if logins.blocked(email, ip) {
		w.Header().Set("Retry-After", "900")
		jsonError(w, http.StatusTooManyRequests, "too many failed login attempts, please try again later")
		return
	}

	user, err := storage.AuthenticateUser(email, data.Password)
	if err != nil {
		if !errors.Is(err, storage.ErrInvalidCredentials) {
			logger.Log().Errorf("[auth] %s", err.Error())
			jsonError(w, http.StatusInternalServerError, "unable to log in")
			return
		}

		logins.fail(email, ip)
		logger.Log().Warnf("[auth] failed login for %q from %s", email, ip)
		jsonError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, expires, err := storage.CreateSession(user.ID)
	if err != nil {
		logger.Log().Errorf("[auth] %s", err.Error())
		jsonError(w, http.StatusInternalServerError, "unable to log in")
		return
	}

	logins.success(email)
	setSessionCookie(w, r, token, expires)

	// the response is for the new session, not the (anonymous) request
	r = tenancy.WithPrincipal(r, &tenancy.Principal{
		UserID:     user.ID,
		Email:      user.Email,
		SuperAdmin: user.IsSuperAdmin,
		Projects:   user.Projects,
	})

	jsonResponse(w, http.StatusOK, statusFor(r))
}

// logoutHandler logs the user out
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookie); err == nil && cookie.Value != "" {
		if err := storage.DeleteSession(cookie.Value); err != nil {
			logger.Log().Errorf("[auth] %s", err.Error())
		}
	}

	setSessionCookie(w, r, "", time.Unix(0, 0))

	jsonResponse(w, http.StatusOK, authStatus{MultiProject: config.MultiProject, Projects: []storage.Project{}})
}

// changePasswordHandler changes the password of the logged in user, which logs out all other sessions
func changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	user, err := currentUser(r)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var data struct {
		Current string
		New     string
	}

	if !decodeJSON(w, r, &data) {
		return
	}

	if !storage.CheckUserPassword(user.ID, data.Current) {
		jsonError(w, http.StatusBadRequest, "the current password is incorrect")
		return
	}

	if err := storage.SetUserPassword(user.ID, data.New); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// changing the password ends all sessions, so start a new one for this browser
	token, expires, err := storage.CreateSession(user.ID)
	if err != nil {
		logger.Log().Errorf("[auth] %s", err.Error())
		jsonError(w, http.StatusInternalServerError, "unable to create session")
		return
	}

	setSessionCookie(w, r, token, expires)

	jsonResponse(w, http.StatusOK, struct{ OK bool }{OK: true})
}
