package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axllent/mailpit/config"
	"github.com/axllent/mailpit/internal/storage"
	"github.com/axllent/mailpit/server/apiv1"
	"github.com/axllent/mailpit/server/websockets"
	"github.com/gorilla/websocket"
)

const (
	testAdminEmail    = "admin@example.com"
	testAdminPassword = "admin-password"
)

// testClient is a http client for the test server, which keeps the login cookie
type testClient struct {
	t    *testing.T
	base string
	http *http.Client
}

func newTestClient(t *testing.T, base string) *testClient {
	jar, _ := cookiejar.New(nil)

	return &testClient{t: t, base: base, http: &http.Client{Jar: jar}}
}

// do sends a request, a body is sent as JSON
func (c *testClient) do(method, path string, body any, headers ...string) (int, []byte) {
	c.t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}

	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	b, _ := io.ReadAll(res.Body)

	return res.StatusCode, b
}

func (c *testClient) expect(status int, method, path string, body any, headers ...string) []byte {
	c.t.Helper()

	got, b := c.do(method, path, body, headers...)
	if got != status {
		c.t.Fatalf("%s %s: expected status %d, got %d: %s", method, path, status, got, string(b))
	}

	return b
}

func (c *testClient) login(email, password string) {
	c.t.Helper()

	c.expect(http.StatusOK, "POST", "/api/v1/auth/login", map[string]string{"Email": email, "Password": password})
}

func setupMultiProject(t *testing.T) *httptest.Server {
	t.Helper()

	config.MultiProject = true
	config.SuperAdminEmail = testAdminEmail
	config.SuperAdminPassword = testAdminPassword
	logins.failures = map[string][]time.Time{}

	setup()

	ts := httptest.NewServer(apiRoutes())

	t.Cleanup(func() {
		ts.Close()
		storage.Close()
		config.MultiProject = false
		config.SuperAdminEmail = ""
		config.SuperAdminPassword = ""
	})

	return ts
}

type createdProject struct {
	storage.Project
	APIKey string
}

func createProject(t *testing.T, admin *testClient, name string) createdProject {
	t.Helper()

	var p createdProject
	if err := json.Unmarshal(admin.expect(http.StatusCreated, "POST", "/api/v1/admin/projects", map[string]string{"Name": name}), &p); err != nil {
		t.Fatal(err)
	}

	if p.ID == "" || !strings.HasPrefix(p.APIKey, "mpk_") {
		t.Fatalf("unexpected project: %+v", p)
	}

	return p
}

func sendWithKey(t *testing.T, c *testClient, status int, subject string, headers ...string) string {
	t.Helper()

	msg := map[string]any{
		"From":    map[string]string{"Email": "from@example.com"},
		"To":      []map[string]string{{"Email": "to@example.com"}},
		"Subject": subject,
		"Text":    "body",
	}

	b := c.expect(status, "POST", "/api/v1/send", msg, headers...)
	if status != http.StatusOK {
		return ""
	}

	var res struct{ ID string }
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}

	return res.ID
}

func messageTotal(t *testing.T, c *testClient, headers ...string) uint64 {
	t.Helper()

	var m apiv1.MessagesSummary
	if err := json.Unmarshal(c.expect(http.StatusOK, "GET", "/api/v1/messages", nil, headers...), &m); err != nil {
		t.Fatal(err)
	}

	return m.Total
}

func TestMultiProjectRequiresAuthentication(t *testing.T) {
	ts := setupMultiProject(t)
	c := newTestClient(t, ts.URL)

	for _, path := range []string{
		"/api/v1/messages", "/api/v1/info", "/api/v1/tags", "/api/v1/webui", "/api/v1/search?query=a",
		"/api/v1/message/latest", "/api/v1/message/abc/raw", "/api/v1/chaos", "/api/events",
		"/api/v1/admin/projects", "/api/v1/admin/users",
	} {
		c.expect(http.StatusUnauthorized, "GET", path, nil)
	}

	c.expect(http.StatusUnauthorized, "POST", "/api/v1/send", map[string]any{})
	c.expect(http.StatusUnauthorized, "DELETE", "/api/v1/messages", nil)
	c.expect(http.StatusUnauthorized, "PUT", "/api/v1/auth/password", map[string]string{})
	c.expect(http.StatusUnauthorized, "GET", "/api/v1/messages", nil, "X-API-Key", "mpk_invalid")
	c.expect(http.StatusUnauthorized, "GET", "/api/v1/messages", nil, "Authorization", "Bearer mpk_invalid")

	// the status is public
	var status authStatus
	if err := json.Unmarshal(c.expect(http.StatusOK, "GET", "/api/v1/auth/status", nil), &status); err != nil {
		t.Fatal(err)
	}
	if !status.MultiProject || status.User != nil {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestMultiProjectLogin(t *testing.T) {
	ts := setupMultiProject(t)
	c := newTestClient(t, ts.URL)

	c.expect(http.StatusUnauthorized, "POST", "/api/v1/auth/login", map[string]string{"Email": testAdminEmail, "Password": "wrong"})
	c.expect(http.StatusUnauthorized, "POST", "/api/v1/auth/login", map[string]string{"Email": "missing@example.com", "Password": "wrong"})
	c.expect(http.StatusBadRequest, "POST", "/api/v1/auth/login", nil)

	res, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(fmt.Sprintf(`{"Email":%q,"Password":%q}`, strings.ToUpper(testAdminEmail), testAdminPassword)))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %d", res.StatusCode)
	}

	var cookie *http.Cookie
	for _, ck := range res.Cookies() {
		if ck.Name == SessionCookie {
			cookie = ck
		}
	}

	if cookie == nil || cookie.Value == "" {
		t.Fatal("no session cookie")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("the cookie must be HttpOnly and SameSite=Strict: %+v", cookie)
	}

	c.login(testAdminEmail, testAdminPassword)

	var status authStatus
	if err := json.Unmarshal(c.expect(http.StatusOK, "GET", "/api/v1/auth/status", nil), &status); err != nil {
		t.Fatal(err)
	}
	if status.User == nil || status.User.Email != testAdminEmail || !status.User.IsSuperAdmin {
		t.Fatalf("unexpected status: %+v", status)
	}

	// logging out ends the session
	c.expect(http.StatusOK, "POST", "/api/v1/auth/logout", nil)
	c.expect(http.StatusUnauthorized, "GET", "/api/v1/admin/projects", nil)

	// and the old cookie no longer works
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/admin/projects", nil)
	req.AddCookie(cookie)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the other session should still be valid, got %d", res.StatusCode)
	}
}

func TestMultiProjectLoginRateLimit(t *testing.T) {
	ts := setupMultiProject(t)
	c := newTestClient(t, ts.URL)

	for range maxEmailFailures {
		c.expect(http.StatusUnauthorized, "POST", "/api/v1/auth/login", map[string]string{"Email": testAdminEmail, "Password": "wrong"})
	}

	// even the correct password is refused until the window has passed
	c.expect(http.StatusTooManyRequests, "POST", "/api/v1/auth/login", map[string]string{"Email": testAdminEmail, "Password": testAdminPassword})

	// other accounts are not affected
	c.expect(http.StatusUnauthorized, "POST", "/api/v1/auth/login", map[string]string{"Email": "other@example.com", "Password": "wrong"})
}

func TestMultiProjectAPIKeys(t *testing.T) {
	ts := setupMultiProject(t)

	admin := newTestClient(t, ts.URL)
	admin.login(testAdminEmail, testAdminPassword)

	a := createProject(t, admin, "Project A")
	b := createProject(t, admin, "Project B")

	anon := newTestClient(t, ts.URL)

	t.Run("messages are stored in the project of the key", func(t *testing.T) {
		idA := sendWithKey(t, anon, http.StatusOK, "for A", "Authorization", "Bearer "+a.APIKey)
		// Basic authentication with the key as the password, like most HTTP clients support
		basic := base64.StdEncoding.EncodeToString([]byte("anything:" + b.APIKey))
		idB := sendWithKey(t, anon, http.StatusOK, "for B", "Authorization", "Basic "+basic)
		sendWithKey(t, anon, http.StatusOK, "for B again", "X-API-Key", b.APIKey)

		assertEqual(t, uint64(1), messageTotal(t, anon, "X-API-Key", a.APIKey), "project A total")
		assertEqual(t, uint64(2), messageTotal(t, anon, "X-API-Key", b.APIKey), "project B total")

		// a key cannot read the messages of another project
		anon.expect(http.StatusOK, "GET", "/api/v1/message/"+idA, nil, "X-API-Key", a.APIKey)
		anon.expect(http.StatusNotFound, "GET", "/api/v1/message/"+idA, nil, "X-API-Key", b.APIKey)
		anon.expect(http.StatusNotFound, "GET", "/api/v1/message/"+idB+"/raw", nil, "X-API-Key", a.APIKey)
		anon.expect(http.StatusNotFound, "GET", "/api/v1/message/"+idB+"/headers", nil, "X-API-Key", a.APIKey)
		anon.expect(http.StatusNotFound, "GET", "/api/v1/message/"+idB+"/part/1", nil, "X-API-Key", a.APIKey)
		anon.expect(http.StatusNotFound, "GET", "/api/v1/message/"+idB+"/html-check", nil, "X-API-Key", a.APIKey)

		// 'latest' is the latest of the project
		var msg struct{ Subject string }
		if err := json.Unmarshal(anon.expect(http.StatusOK, "GET", "/api/v1/message/latest", nil, "X-API-Key", a.APIKey), &msg); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, "for A", msg.Subject, "latest message of project A")

		// search & delete only apply to the project
		var res apiv1.MessagesSummary
		if err := json.Unmarshal(anon.expect(http.StatusOK, "GET", "/api/v1/search?query=subject:for", nil, "X-API-Key", a.APIKey), &res); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, uint64(1), res.MessagesCount, "search results in project A")

		anon.expect(http.StatusOK, "DELETE", "/api/v1/messages", map[string]any{"IDs": []string{idB}}, "X-API-Key", a.APIKey)
		assertEqual(t, uint64(2), messageTotal(t, anon, "X-API-Key", b.APIKey), "project B total after A tried to delete its message")

		anon.expect(http.StatusOK, "DELETE", "/api/v1/messages", nil, "X-API-Key", a.APIKey)
		assertEqual(t, uint64(0), messageTotal(t, anon, "X-API-Key", a.APIKey), "project A total after delete all")
		assertEqual(t, uint64(2), messageTotal(t, anon, "X-API-Key", b.APIKey), "project B total after A deleted all")
	})

	t.Run("the project header is ignored for keys", func(t *testing.T) {
		assertEqual(t, uint64(2), messageTotal(t, anon, "X-API-Key", b.APIKey, projectHeader, a.Slug), "total of the key's project")
	})

	t.Run("keys cannot use the admin api", func(t *testing.T) {
		anon.expect(http.StatusForbidden, "GET", "/api/v1/admin/projects", nil, "X-API-Key", a.APIKey)
		anon.expect(http.StatusForbidden, "POST", "/api/v1/admin/users", map[string]any{}, "X-API-Key", a.APIKey)
		anon.expect(http.StatusForbidden, "PUT", "/api/v1/chaos", map[string]any{}, "X-API-Key", a.APIKey)
	})

	t.Run("disabling a project", func(t *testing.T) {
		admin.expect(http.StatusOK, "PUT", "/api/v1/admin/projects/"+a.ID, map[string]any{"Disabled": true})

		// the API key no longer works, for reading or sending
		anon.expect(http.StatusForbidden, "GET", "/api/v1/messages", nil, "X-API-Key", a.APIKey)
		anon.expect(http.StatusForbidden, "GET", "/api/v1/messages", nil, "Authorization", "Bearer "+a.APIKey)
		sendWithKey(t, anon, http.StatusForbidden, "disabled", "X-API-Key", a.APIKey)

		// other projects are not affected
		assertEqual(t, uint64(2), messageTotal(t, anon, "X-API-Key", b.APIKey), "project B total")

		admin.expect(http.StatusOK, "PUT", "/api/v1/admin/projects/"+a.ID, map[string]any{"Disabled": false})
		assertEqual(t, uint64(0), messageTotal(t, anon, "X-API-Key", a.APIKey), "project A works again")
		sendWithKey(t, anon, http.StatusOK, "enabled", "X-API-Key", a.APIKey)
	})

	t.Run("rotating the key", func(t *testing.T) {
		var rotated createdProject
		if err := json.Unmarshal(admin.expect(http.StatusOK, "POST", "/api/v1/admin/projects/"+a.ID+"/api-key", nil), &rotated); err != nil {
			t.Fatal(err)
		}

		if rotated.APIKey == "" || rotated.APIKey == a.APIKey {
			t.Fatal("the key was not changed")
		}

		anon.expect(http.StatusUnauthorized, "GET", "/api/v1/messages", nil, "X-API-Key", a.APIKey)
		assertEqual(t, uint64(1), messageTotal(t, anon, "X-API-Key", rotated.APIKey), "messages are kept when rotating the key")
	})

	t.Run("the key is only returned when generated", func(t *testing.T) {
		body := string(admin.expect(http.StatusOK, "GET", "/api/v1/admin/projects", nil))
		if strings.Contains(body, "mpk_") || strings.Contains(body, "KeyHash") {
			t.Fatalf("the project list must not contain API keys: %s", body)
		}
	})
}

func TestMultiProjectUsers(t *testing.T) {
	ts := setupMultiProject(t)

	admin := newTestClient(t, ts.URL)
	admin.login(testAdminEmail, testAdminPassword)

	a := createProject(t, admin, "Project A")
	b := createProject(t, admin, "Project B")

	idA := sendWithKey(t, newTestClient(t, ts.URL), http.StatusOK, "A", "X-API-Key", a.APIKey)
	idB := sendWithKey(t, newTestClient(t, ts.URL), http.StatusOK, "B", "X-API-Key", b.APIKey)

	var user storage.User
	if err := json.Unmarshal(admin.expect(http.StatusCreated, "POST", "/api/v1/admin/users", map[string]any{
		"Email": "user@example.com", "Name": "User", "Password": "user-password", "Projects": []string{b.ID},
	}), &user); err != nil {
		t.Fatal(err)
	}

	admin.expect(http.StatusBadRequest, "POST", "/api/v1/admin/users", map[string]any{"Email": "user@example.com", "Password": "user-password"})
	admin.expect(http.StatusBadRequest, "POST", "/api/v1/admin/users", map[string]any{"Email": "x@example.com", "Password": "short"})

	u := newTestClient(t, ts.URL)
	u.login("user@example.com", "user-password")

	t.Run("status lists the projects of the user", func(t *testing.T) {
		var status authStatus
		if err := json.Unmarshal(u.expect(http.StatusOK, "GET", "/api/v1/auth/status", nil), &status); err != nil {
			t.Fatal(err)
		}

		if status.User == nil || status.User.IsSuperAdmin || len(status.Projects) != 1 || status.Projects[0].ID != b.ID {
			t.Fatalf("unexpected status: %+v", status)
		}
	})

	t.Run("a project must be selected", func(t *testing.T) {
		u.expect(http.StatusBadRequest, "GET", "/api/v1/messages", nil)
		assertEqual(t, uint64(1), messageTotal(t, u, projectHeader, b.Slug), "selected using the slug")
		assertEqual(t, uint64(1), messageTotal(t, u, projectHeader, b.ID), "selected using the ID")

		var m apiv1.MessagesSummary
		if err := json.Unmarshal(u.expect(http.StatusOK, "GET", "/api/v1/messages?project="+b.Slug, nil), &m); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, uint64(1), m.Total, "selected using the query argument")
	})

	t.Run("other projects are not accessible", func(t *testing.T) {
		u.expect(http.StatusForbidden, "GET", "/api/v1/messages", nil, projectHeader, a.Slug)
		u.expect(http.StatusForbidden, "GET", "/api/v1/messages?project="+a.Slug, nil)
		u.expect(http.StatusForbidden, "GET", "/api/v1/messages", nil, projectHeader, "missing")

		// messages are found by their ID without selecting a project, but only in the projects of the user
		u.expect(http.StatusOK, "GET", "/api/v1/message/"+idB, nil)
		u.expect(http.StatusOK, "GET", "/api/v1/message/"+idB+"/raw", nil)
		u.expect(http.StatusNotFound, "GET", "/api/v1/message/"+idA, nil)
		u.expect(http.StatusNotFound, "GET", "/api/v1/message/"+idA+"/raw", nil)
		u.expect(http.StatusNotFound, "GET", "/api/v1/message/"+idA, nil, projectHeader, b.Slug)
		u.expect(http.StatusForbidden, "GET", "/api/v1/message/"+idB, nil, projectHeader, a.Slug)
		// a message of another project is not found even if the user may access both
		admin.expect(http.StatusNotFound, "GET", "/api/v1/message/"+idB, nil, projectHeader, a.Slug)
		admin.expect(http.StatusOK, "GET", "/api/v1/message/"+idB, nil, projectHeader, b.Slug)
		u.expect(http.StatusNotFound, "GET", "/view/"+idA+".html", nil)

		// changes only apply to the project
		u.expect(http.StatusOK, "DELETE", "/api/v1/messages", map[string]any{"IDs": []string{idA}}, projectHeader, b.Slug)
		assertEqual(t, uint64(1), messageTotal(t, admin, projectHeader, a.Slug), "project A total")
	})

	t.Run("users cannot administrate", func(t *testing.T) {
		u.expect(http.StatusForbidden, "GET", "/api/v1/admin/projects", nil)
		u.expect(http.StatusForbidden, "POST", "/api/v1/admin/projects", map[string]any{"Name": "x"})
		u.expect(http.StatusForbidden, "PUT", "/api/v1/admin/projects/"+b.ID, map[string]any{"Disabled": true})
		u.expect(http.StatusForbidden, "POST", "/api/v1/admin/projects/"+b.ID+"/api-key", nil)
		u.expect(http.StatusForbidden, "DELETE", "/api/v1/admin/projects/"+b.ID, nil)
		u.expect(http.StatusForbidden, "GET", "/api/v1/admin/users", nil)
		u.expect(http.StatusForbidden, "PUT", "/api/v1/admin/users/"+user.ID, map[string]any{"IsSuperAdmin": true})
		u.expect(http.StatusForbidden, "PUT", "/api/v1/chaos", map[string]any{})

		var conf apiv1.WebUIConfiguration
		if err := json.Unmarshal(u.expect(http.StatusOK, "GET", "/api/v1/webui", nil), &conf); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("super admins access every project", func(t *testing.T) {
		assertEqual(t, uint64(1), messageTotal(t, admin, projectHeader, a.Slug), "project A")
		assertEqual(t, uint64(1), messageTotal(t, admin, projectHeader, b.Slug), "project B")
		admin.expect(http.StatusBadRequest, "GET", "/api/v1/messages", nil)
	})

	t.Run("assigning projects", func(t *testing.T) {
		admin.expect(http.StatusOK, "PUT", "/api/v1/admin/users/"+user.ID, map[string]any{"Projects": []string{a.ID, b.ID}})
		assertEqual(t, uint64(1), messageTotal(t, u, projectHeader, a.Slug), "project A after being assigned")

		admin.expect(http.StatusOK, "PUT", "/api/v1/admin/users/"+user.ID, map[string]any{"Projects": []string{}})
		u.expect(http.StatusForbidden, "GET", "/api/v1/messages", nil, projectHeader, a.Slug)

		admin.expect(http.StatusBadRequest, "PUT", "/api/v1/admin/users/"+user.ID, map[string]any{"Projects": []string{"missing"}})
		admin.expect(http.StatusNotFound, "PUT", "/api/v1/admin/users/missing", map[string]any{"Name": "x"})
	})

	t.Run("a disabled project can still be read by its users", func(t *testing.T) {
		admin.expect(http.StatusOK, "PUT", "/api/v1/admin/users/"+user.ID, map[string]any{"Projects": []string{b.ID}})
		admin.expect(http.StatusOK, "PUT", "/api/v1/admin/projects/"+b.ID, map[string]any{"Disabled": true})
		assertEqual(t, uint64(1), messageTotal(t, u, projectHeader, b.Slug), "disabled project total")
		newTestClient(t, ts.URL).expect(http.StatusForbidden, "GET", "/api/v1/messages", nil, "X-API-Key", b.APIKey)
	})

	t.Run("changing the password", func(t *testing.T) {
		u.expect(http.StatusBadRequest, "PUT", "/api/v1/auth/password", map[string]string{"Current": "wrong", "New": "new-password"})
		u.expect(http.StatusBadRequest, "PUT", "/api/v1/auth/password", map[string]string{"Current": "user-password", "New": "short"})

		other := newTestClient(t, ts.URL)
		other.login("user@example.com", "user-password")

		u.expect(http.StatusOK, "PUT", "/api/v1/auth/password", map[string]string{"Current": "user-password", "New": "new-password"})

		// this browser stays logged in, all others are logged out
		u.expect(http.StatusOK, "GET", "/api/v1/auth/status", nil)
		other.expect(http.StatusUnauthorized, "GET", "/api/v1/messages", nil)
		var status authStatus
		if err := json.Unmarshal(other.expect(http.StatusOK, "GET", "/api/v1/auth/status", nil), &status); err != nil {
			t.Fatal(err)
		}
		if status.User != nil {
			t.Fatal("the other session should have been logged out")
		}

		newTestClient(t, ts.URL).expect(http.StatusUnauthorized, "POST", "/api/v1/auth/login", map[string]string{"Email": "user@example.com", "Password": "user-password"})
		newTestClient(t, ts.URL).login("user@example.com", "new-password")
	})

	t.Run("disabling a user logs the user out", func(t *testing.T) {
		admin.expect(http.StatusOK, "PUT", "/api/v1/admin/users/"+user.ID, map[string]any{"Disabled": true})

		var status authStatus
		if err := json.Unmarshal(u.expect(http.StatusOK, "GET", "/api/v1/auth/status", nil), &status); err != nil {
			t.Fatal(err)
		}
		if status.User != nil {
			t.Fatal("the disabled user is still logged in")
		}

		newTestClient(t, ts.URL).expect(http.StatusUnauthorized, "POST", "/api/v1/auth/login", map[string]string{"Email": "user@example.com", "Password": "new-password"})
	})

	t.Run("super admins cannot lock themselves out", func(t *testing.T) {
		var users []storage.User
		if err := json.Unmarshal(admin.expect(http.StatusOK, "GET", "/api/v1/admin/users", nil), &users); err != nil {
			t.Fatal(err)
		}

		var adminID string
		for _, usr := range users {
			if usr.Email == testAdminEmail {
				adminID = usr.ID
			}
		}

		admin.expect(http.StatusBadRequest, "PUT", "/api/v1/admin/users/"+adminID, map[string]any{"Disabled": true})
		admin.expect(http.StatusBadRequest, "PUT", "/api/v1/admin/users/"+adminID, map[string]any{"IsSuperAdmin": false})
		admin.expect(http.StatusBadRequest, "DELETE", "/api/v1/admin/users/"+adminID, nil)
	})

	t.Run("deleting a user", func(t *testing.T) {
		admin.expect(http.StatusOK, "DELETE", "/api/v1/admin/users/"+user.ID, nil)
		admin.expect(http.StatusNotFound, "DELETE", "/api/v1/admin/users/"+user.ID, nil)
	})

	t.Run("deleting a project deletes its messages", func(t *testing.T) {
		admin.expect(http.StatusOK, "DELETE", "/api/v1/admin/projects/"+b.ID, nil)
		admin.expect(http.StatusNotFound, "DELETE", "/api/v1/admin/projects/"+b.ID, nil)
		admin.expect(http.StatusForbidden, "GET", "/api/v1/messages", nil, projectHeader, b.Slug)
		newTestClient(t, ts.URL).expect(http.StatusUnauthorized, "GET", "/api/v1/messages", nil, "X-API-Key", b.APIKey)
		assertEqual(t, uint64(0), storage.CountTotal(b.ID), "messages of the deleted project")
		assertEqual(t, uint64(1), storage.CountTotal(a.ID), "messages of the other project")
	})
}

func TestMultiProjectWebsocketIsolation(t *testing.T) {
	ts := setupMultiProject(t)

	websockets.MessageHub = websockets.NewHub()
	websockets.SetCheckOriginFunc(func(*http.Request) bool { return true })
	go websockets.MessageHub.Run()

	admin := newTestClient(t, ts.URL)
	admin.login(testAdminEmail, testAdminPassword)

	a := createProject(t, admin, "Project A")
	b := createProject(t, admin, "Project B")

	dial := func(header http.Header) *websocket.Conn {
		conn, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/api/events", header)
		if err != nil {
			t.Fatalf("websocket: %v (%v)", err, res)
		}
		t.Cleanup(func() { _ = conn.Close() })

		return conn
	}

	connA := dial(http.Header{"X-Api-Key": {a.APIKey}})
	connB := dial(http.Header{"X-Api-Key": {b.APIKey}})

	// wait until both clients are registered
	deadline := time.Now().Add(2 * time.Second)
	for websockets.MessageHub.ClientCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// the websocket requires authentication
	if _, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/api/events", nil); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatal("an unauthenticated websocket should be refused")
	}

	sendWithKey(t, newTestClient(t, ts.URL), http.StatusOK, "for A", "X-API-Key", a.APIKey)

	// reads the next message of a connection of the given type
	next := func(conn *websocket.Conn, wantType string, wait time.Duration) bool {
		_ = conn.SetReadDeadline(time.Now().Add(wait))

		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				return false
			}

			var n websockets.WebsocketNotification
			if err := json.Unmarshal(b, &n); err == nil && n.Type == wantType {
				return true
			}
		}
	}

	if !next(connA, "new", 2*time.Second) {
		t.Fatal("project A did not receive its new message")
	}

	if next(connB, "new", 500*time.Millisecond) {
		t.Fatal("project B received a message of project A")
	}
}
