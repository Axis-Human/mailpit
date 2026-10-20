package server

import (
	"errors"
	"net/http"

	"github.com/axllent/mailpit/internal/logger"
	"github.com/axllent/mailpit/internal/storage"
	"github.com/axllent/mailpit/internal/tenancy"
)

// projectWithKey is a project including its (newly generated) API key
type projectWithKey struct {
	storage.Project
	// The API key is only returned when it is generated
	APIKey string
}

// adminError writes an error response for a failed admin action
func adminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrProjectNotFound), errors.Is(err, storage.ErrUserNotFound):
		jsonError(w, http.StatusNotFound, err.Error())
	default:
		jsonError(w, http.StatusBadRequest, err.Error())
	}
}

// listProjectsHandler returns all projects
func listProjectsHandler(w http.ResponseWriter, _ *http.Request) {
	projects, err := storage.ListProjects()
	if err != nil {
		adminError(w, err)
		return
	}

	jsonResponse(w, http.StatusOK, projects)
}

// createProjectHandler creates a project and returns its API key
func createProjectHandler(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Name string
		Slug string
	}

	if !decodeJSON(w, r, &data) {
		return
	}

	project, key, err := storage.CreateProject(data.Name, data.Slug)
	if err != nil {
		adminError(w, err)
		return
	}

	logger.Log().Infof("[projects] created project %s (%s)", project.Slug, project.ID)

	jsonResponse(w, http.StatusCreated, projectWithKey{Project: *project, APIKey: key})
}

// updateProjectHandler changes the name, slug or disabled status of a project
func updateProjectHandler(w http.ResponseWriter, r *http.Request) {
	var data storage.ProjectUpdate

	if !decodeJSON(w, r, &data) {
		return
	}

	project, err := storage.UpdateProject(r.PathValue("id"), data)
	if err != nil {
		adminError(w, err)
		return
	}

	jsonResponse(w, http.StatusOK, project)
}

// rotateProjectKeyHandler generates a new API key for a project
func rotateProjectKeyHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	project, err := storage.GetProject(id)
	if err != nil {
		adminError(w, err)
		return
	}

	key, err := storage.RotateProjectAPIKey(id)
	if err != nil {
		adminError(w, err)
		return
	}

	// the hint changed, so get the project again
	if updated, err := storage.GetProject(id); err == nil {
		project = updated
	}

	logger.Log().Infof("[projects] rotated the API key of project %s (%s)", project.Slug, project.ID)

	jsonResponse(w, http.StatusOK, projectWithKey{Project: *project, APIKey: key})
}

// deleteProjectHandler deletes a project and all of its messages
func deleteProjectHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := storage.DeleteProject(id); err != nil {
		adminError(w, err)
		return
	}

	logger.Log().Infof("[projects] deleted project %s", id)

	jsonResponse(w, http.StatusOK, struct{ OK bool }{OK: true})
}

// listUsersHandler returns all users
func listUsersHandler(w http.ResponseWriter, _ *http.Request) {
	users, err := storage.ListUsers()
	if err != nil {
		adminError(w, err)
		return
	}

	jsonResponse(w, http.StatusOK, users)
}

// createUserHandler creates a user
func createUserHandler(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Email        string
		Name         string
		Password     string
		IsSuperAdmin bool
		Projects     []string
	}

	if !decodeJSON(w, r, &data) {
		return
	}

	user, err := storage.CreateUser(data.Email, data.Name, data.Password, data.IsSuperAdmin, data.Projects)
	if err != nil {
		adminError(w, err)
		return
	}

	logger.Log().Infof("[users] created user %s", user.Email)

	jsonResponse(w, http.StatusCreated, user)
}

// updateUserHandler changes a user, optionally replacing the projects the user has access to
func updateUserHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var data struct {
		storage.UserUpdate
		// when set (even if empty) replaces the assigned projects
		Projects *[]string
	}

	if !decodeJSON(w, r, &data) {
		return
	}

	if p := tenancy.FromRequest(r); p != nil && p.UserID == id {
		if (data.Disabled != nil && *data.Disabled) || (data.IsSuperAdmin != nil && !*data.IsSuperAdmin) {
			jsonError(w, http.StatusBadRequest, "you cannot disable or demote your own account")
			return
		}
	}

	if data.Projects != nil {
		if err := storage.SetUserProjects(id, *data.Projects); err != nil {
			adminError(w, err)
			return
		}
	}

	if _, err := storage.UpdateUser(id, data.UserUpdate); err != nil {
		adminError(w, err)
		return
	}

	user, err := storage.GetUser(id)
	if err != nil {
		adminError(w, err)
		return
	}

	jsonResponse(w, http.StatusOK, user)
}

// deleteUserHandler deletes a user
func deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if p := tenancy.FromRequest(r); p != nil && p.UserID == id {
		jsonError(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}

	if err := storage.DeleteUser(id); err != nil {
		adminError(w, err)
		return
	}

	jsonResponse(w, http.StatusOK, struct{ OK bool }{OK: true})
}
