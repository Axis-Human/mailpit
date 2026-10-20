package storage

import (
	"errors"
	"strings"
	"testing"

	"github.com/axllent/mailpit/config"
)

func TestProjectsIsolation(t *testing.T) {
	setup("")
	defer Close()

	a, _, err := CreateProject("Project A", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := CreateProject("Project B", "")
	if err != nil {
		t.Fatal(err)
	}

	assertEqual(t, "project-a", a.Slug, "slug should be generated from the name")

	// 3 messages in A (one tagged), 2 in B
	var aIDs, bIDs []string
	for range 3 {
		id, err := Store(a.ID, &testTextEmail, nil)
		if err != nil {
			t.Fatal(err)
		}
		aIDs = append(aIDs, id)
	}
	for range 2 {
		id, err := Store(b.ID, &testMimeEmail, nil)
		if err != nil {
			t.Fatal(err)
		}
		bIDs = append(bIDs, id)
	}

	if _, err := SetMessageTags(a.ID, aIDs[0], []string{"only-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := SetMessageTags(b.ID, bIDs[0], []string{"only-b", "shared"}); err != nil {
		t.Fatal(err)
	}
	if _, err := SetMessageTags(a.ID, aIDs[1], []string{"shared"}); err != nil {
		t.Fatal(err)
	}

	t.Run("counts and lists", func(t *testing.T) {
		assertEqual(t, uint64(3), CountTotal(a.ID), "project A total")
		assertEqual(t, uint64(2), CountTotal(b.ID), "project B total")
		assertEqual(t, uint64(5), CountTotal(AllProjects), "all projects total")
		assertEqual(t, uint64(0), CountTotal(""), "no messages in the single mailbox")

		list, err := List(a.ID, 0, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, 3, len(list), "project A list")

		list, err = List(b.ID, 0, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, 2, len(list), "project B list")
	})

	t.Run("search", func(t *testing.T) {
		// both emails share content, so all of a project's messages match but never the other's
		_, total, err := Search(b.ID, "is:unread", "", 0, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, 2, total, "project B unread search")

		unread, err := SearchUnreadCount(a.ID, "is:unread", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, int64(3), unread, "project A unread search count")

		_, total, err = Search(a.ID, "tag:only-b", "", 0, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, 0, total, "project A cannot find the tag of project B")
	})

	t.Run("tags are per project", func(t *testing.T) {
		assertEqual(t, "only-a,shared", strings.Join(GetAllTags(a.ID), ","), "project A tags")
		assertEqual(t, "only-b,shared", strings.Join(GetAllTags(b.ID), ","), "project B tags")

		counts := GetAllTagsCount(a.ID)
		assertEqual(t, int64(1), counts["shared"], "project A shared tag count")
		assertEqual(t, 2, len(counts), "project A tag count")

		// renaming in A does not touch B
		if err := RenameTag(a.ID, "shared", "renamed"); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, "only-b,shared", strings.Join(GetAllTags(b.ID), ","), "project B tags after rename in A")
		assertEqual(t, "only-a,renamed", strings.Join(GetAllTags(a.ID), ","), "project A tags after rename")

		if err := RenameTag(a.ID, "only-b", "x"); err == nil {
			t.Fatal("project A should not be able to rename a tag of project B")
		}
		if err := DeleteTag(a.ID, "only-b"); err == nil {
			t.Fatal("project A should not be able to delete a tag of project B")
		}
	})

	t.Run("messages of other projects are not accessible", func(t *testing.T) {
		if _, err := GetMessage(a.ID, bIDs[0]); err == nil {
			t.Fatal("project A can read a message of project B")
		}
		if _, err := GetMessageRaw(a.ID, bIDs[0]); err == nil {
			t.Fatal("project A can read the raw message of project B")
		}
		if _, err := GetAttachmentPart(a.ID, bIDs[0], "1"); err == nil {
			t.Fatal("project A can read an attachment of project B")
		}
		if _, err := GetMetadata(a.ID, bIDs[0]); err == nil {
			t.Fatal("project A can read the metadata of project B")
		}
		if _, err := SetMessageTags(a.ID, bIDs[0], []string{"hijack"}); err == nil {
			t.Fatal("project A can tag a message of project B")
		}

		if _, err := GetMessage(b.ID, bIDs[0]); err != nil {
			t.Fatalf("project B cannot read its own message: %s", err)
		}
	})

	t.Run("read status and deletes are scoped", func(t *testing.T) {
		// reading a message in the previous test marked it as read, so B has 1 unread message
		assertEqual(t, uint64(1), CountUnread(b.ID), "project B unread")

		// A cannot change or delete the messages of B
		if err := MarkRead(a.ID, bIDs); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, uint64(1), CountUnread(b.ID), "project B unread after A marked its IDs as read")

		if err := DeleteMessages(a.ID, bIDs); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, uint64(2), CountTotal(b.ID), "project B total after A deleted its IDs")

		// marking all as read applies to the project only
		if err := MarkAllRead(a.ID); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, uint64(0), CountUnread(a.ID), "project A unread")
		assertEqual(t, uint64(1), CountUnread(b.ID), "project B unread")

		if err := SetSearchReadStatus(a.ID, "is:read", "", false); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, uint64(3), CountUnread(a.ID), "project A unread after marking unread")
		assertEqual(t, uint64(1), CountUnread(b.ID), "project B unread")

		// deleting by search applies to the project only
		if err := DeleteSearch(a.ID, "is:unread", ""); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, uint64(0), CountTotal(a.ID), "project A total after delete search")
		assertEqual(t, uint64(2), CountTotal(b.ID), "project B total after delete search")
	})

	t.Run("delete all", func(t *testing.T) {
		for range 2 {
			if _, err := Store(a.ID, &testTextEmail, nil); err != nil {
				t.Fatal(err)
			}
		}

		if err := DeleteAllMessages(a.ID); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, uint64(0), CountTotal(a.ID), "project A total")
		assertEqual(t, uint64(2), CountTotal(b.ID), "project B total")
		assertEqual(t, "only-b,shared", strings.Join(GetAllTags(b.ID), ","), "project B tags after A deleted all")
	})
}

func TestProjectsPrunePerProject(t *testing.T) {
	setup("")
	defer Close()

	a, _, _ := CreateProject("Prune A", "prune-a")
	b, _, _ := CreateProject("Prune B", "prune-b")

	for range 5 {
		if _, err := Store(a.ID, &testTextEmail, nil); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if _, err := Store(b.ID, &testTextEmail, nil); err != nil {
			t.Fatal(err)
		}
	}

	config.MaxMessages = 3
	defer func() { config.MaxMessages = 0 }()

	pruneMessages()

	// the limit applies to each project, so B keeps all of its messages
	assertEqual(t, uint64(3), CountTotal(a.ID), "project A total after pruning")
	assertEqual(t, uint64(2), CountTotal(b.ID), "project B total after pruning")
}

func TestProjectAPIKeys(t *testing.T) {
	setup("")
	defer Close()

	p, key, err := CreateProject("Keys", "keys")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(key, "mpk_") {
		t.Fatalf("unexpected API key format: %s", key)
	}
	assertEqual(t, key[len(key)-4:], p.KeyHint, "key hint")

	// the key itself is never stored
	var stored int
	if err := db.QueryRow(`SELECT COUNT(*) FROM `+tenant("projects")+` WHERE KeyHash = ? OR KeyHash LIKE ?`, key, "%"+key+"%").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, stored, "the API key must not be stored in plain text")

	found, err := ProjectByAPIKey(key)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, p.ID, found.ID, "project found by API key")

	for _, invalid := range []string{"", "mpk_", "mpk_nope", "other", strings.Repeat("a", 500)} {
		if _, err := ProjectByAPIKey(invalid); !errors.Is(err, ErrProjectNotFound) {
			t.Fatalf("API key %q should not be found, got %v", invalid, err)
		}
	}

	t.Run("disable and enable", func(t *testing.T) {
		disabled := true
		if _, err := UpdateProject(p.ID, ProjectUpdate{Disabled: &disabled}); err != nil {
			t.Fatal(err)
		}

		found, err := ProjectByAPIKey(key)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, true, found.Disabled, "the key resolves to a disabled project immediately")
		assertEqual(t, false, ProjectEnabled(p.ID), "project enabled")

		disabled = false
		if _, err := UpdateProject(p.ID, ProjectUpdate{Disabled: &disabled}); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, true, ProjectEnabled(p.ID), "project enabled again")
	})

	t.Run("rotate", func(t *testing.T) {
		newKey, err := RotateProjectAPIKey(p.ID)
		if err != nil {
			t.Fatal(err)
		}

		if newKey == key {
			t.Fatal("the key was not changed")
		}

		if _, err := ProjectByAPIKey(key); !errors.Is(err, ErrProjectNotFound) {
			t.Fatal("the old key still works after rotating")
		}

		if _, err := ProjectByAPIKey(newKey); err != nil {
			t.Fatal("the new key does not work")
		}

		if _, err := RotateProjectAPIKey("missing"); !errors.Is(err, ErrProjectNotFound) {
			t.Fatal("rotating the key of a missing project should fail")
		}
	})

	t.Run("slugs", func(t *testing.T) {
		if _, _, err := CreateProject("Duplicate", "keys"); err == nil {
			t.Fatal("duplicate slugs should not be allowed")
		}
		if _, _, err := CreateProject("Duplicate", "KEYS"); err == nil {
			t.Fatal("slugs are case insensitive")
		}
		if _, _, err := CreateProject("Bad", "has space"); err == nil {
			t.Fatal("invalid slug")
		}
		if _, _, err := CreateProject(" ", ""); err == nil {
			t.Fatal("a name is required")
		}

		assertEqual(t, "my-project-2", NewProjectSlug("  My Project_2!! "), "generated slug")
	})

	t.Run("delete", func(t *testing.T) {
		if _, err := Store(p.ID, &testTextEmail, nil); err != nil {
			t.Fatal(err)
		}

		if err := DeleteProject(p.ID); err != nil {
			t.Fatal(err)
		}

		assertEqual(t, uint64(0), CountTotal(p.ID), "messages are deleted with the project")

		if _, err := GetProject(p.ID); !errors.Is(err, ErrProjectNotFound) {
			t.Fatal("the project still exists")
		}
	})
}

func TestUsersAndSessions(t *testing.T) {
	setup("")
	defer Close()

	config.SessionHours = 1

	p1, _, _ := CreateProject("One", "one")
	p2, _, _ := CreateProject("Two", "two")

	admin, err := CreateUser("Admin@Example.com", "Admin", "password-1", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "admin@example.com", admin.Email, "emails are lowercased")

	user, err := CreateUser("user@example.com", "User", "password-2", false, []string{p1.ID})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("validation", func(t *testing.T) {
		if _, err := CreateUser("user@example.com", "", "password-3", false, nil); err == nil {
			t.Fatal("duplicate emails should not be allowed")
		}
		if _, err := CreateUser("USER@example.com", "", "password-3", false, nil); err == nil {
			t.Fatal("emails are case insensitive")
		}
		if _, err := CreateUser("not-an-email", "", "password-3", false, nil); err == nil {
			t.Fatal("invalid email")
		}
		if _, err := CreateUser("short@example.com", "", "short", false, nil); err == nil {
			t.Fatal("short password")
		}
		if _, err := CreateUser("bad-project@example.com", "", "password-3", false, []string{"missing"}); err == nil {
			t.Fatal("unknown project")
		}
		if _, err := GetUserByEmail("bad-project@example.com"); err == nil {
			t.Fatal("the user should not exist after failing to assign the projects")
		}
	})

	t.Run("authentication", func(t *testing.T) {
		if _, err := AuthenticateUser("user@example.com", "password-2"); err != nil {
			t.Fatalf("valid login failed: %s", err)
		}
		if _, err := AuthenticateUser("USER@example.com ", "password-2"); err != nil {
			t.Fatalf("emails should be case insensitive: %s", err)
		}
		if _, err := AuthenticateUser("user@example.com", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("wrong password accepted")
		}
		if _, err := AuthenticateUser("missing@example.com", "password-2"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("missing user accepted")
		}
	})

	t.Run("project access", func(t *testing.T) {
		assertEqual(t, true, UserCanAccessProject(user, p1.ID), "user access to project 1")
		assertEqual(t, false, UserCanAccessProject(user, p2.ID), "user access to project 2")
		assertEqual(t, true, UserCanAccessProject(admin, p2.ID), "super admin access to project 2")
		assertEqual(t, false, UserCanAccessProject(admin, "missing"), "super admin access to a missing project")

		projects, err := UserProjects(user)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, 1, len(projects), "projects of the user")

		projects, err = UserProjects(admin)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, 2, len(projects), "projects of the super admin")

		if err := SetUserProjects(user.ID, []string{p2.ID, p2.ID}); err != nil {
			t.Fatal(err)
		}
		u, _ := GetUser(user.ID)
		assertEqual(t, "["+p2.ID+"]", "["+strings.Join(u.Projects, ",")+"]", "replaced projects")

		if err := SetUserProjects(user.ID, []string{p1.ID}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("sessions", func(t *testing.T) {
		token, _, err := CreateSession(user.ID)
		if err != nil {
			t.Fatal(err)
		}

		var plain int
		if err := db.QueryRow(`SELECT COUNT(*) FROM `+tenant("sessions")+` WHERE TokenHash = ?`, token).Scan(&plain); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, 0, plain, "the session token must not be stored in plain text")

		u, err := SessionUser(token)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, user.ID, u.ID, "session user")

		if _, err := SessionUser(token + "x"); err == nil {
			t.Fatal("invalid token accepted")
		}

		// expired sessions are not valid
		if _, err := db.Exec(`UPDATE ` + tenant("sessions") + ` SET Expires = 1`); err != nil {
			t.Fatal(err)
		}
		if _, err := SessionUser(token); err == nil {
			t.Fatal("expired session accepted")
		}

		token, _, _ = CreateSession(user.ID)
		if err := DeleteSession(token); err != nil {
			t.Fatal(err)
		}
		if _, err := SessionUser(token); err == nil {
			t.Fatal("session still valid after logout")
		}
	})

	t.Run("password change and disabling end sessions", func(t *testing.T) {
		token, _, _ := CreateSession(user.ID)

		if err := SetUserPassword(user.ID, "password-new"); err != nil {
			t.Fatal(err)
		}
		if _, err := SessionUser(token); err == nil {
			t.Fatal("session still valid after changing the password")
		}
		if _, err := AuthenticateUser(user.Email, "password-2"); err == nil {
			t.Fatal("old password still works")
		}
		if !CheckUserPassword(user.ID, "password-new") {
			t.Fatal("new password does not work")
		}

		token, _, _ = CreateSession(user.ID)
		disabled := true
		if _, err := UpdateUser(user.ID, UserUpdate{Disabled: &disabled}); err != nil {
			t.Fatal(err)
		}
		if _, err := SessionUser(token); err == nil {
			t.Fatal("session still valid for a disabled user")
		}
		if _, err := AuthenticateUser(user.Email, "password-new"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("disabled user can log in")
		}

		disabled = false
		if _, err := UpdateUser(user.ID, UserUpdate{Disabled: &disabled}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("last super admin", func(t *testing.T) {
		no := false
		if _, err := UpdateUser(admin.ID, UserUpdate{IsSuperAdmin: &no}); !errors.Is(err, ErrLastSuperAdmin) {
			t.Fatalf("the last super admin was demoted: %v", err)
		}
		yes := true
		if _, err := UpdateUser(admin.ID, UserUpdate{Disabled: &yes}); !errors.Is(err, ErrLastSuperAdmin) {
			t.Fatalf("the last super admin was disabled: %v", err)
		}
		if err := DeleteUser(admin.ID); !errors.Is(err, ErrLastSuperAdmin) {
			t.Fatalf("the last super admin was deleted: %v", err)
		}

		// with a second super admin the first can be removed
		second, err := CreateUser("second@example.com", "", "password-4", true, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := DeleteUser(admin.ID); err != nil {
			t.Fatal(err)
		}
		if err := DeleteUser(second.ID); !errors.Is(err, ErrLastSuperAdmin) {
			t.Fatal("the last super admin was deleted")
		}
	})

	t.Run("delete user", func(t *testing.T) {
		token, _, _ := CreateSession(user.ID)
		if err := DeleteUser(user.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := SessionUser(token); err == nil {
			t.Fatal("session still valid for a deleted user")
		}
		if _, err := GetUser(user.ID); !errors.Is(err, ErrUserNotFound) {
			t.Fatal("user still exists")
		}
	})
}

func TestEnsureSuperAdmin(t *testing.T) {
	setup("")
	defer Close()

	config.SuperAdminEmail = "root@example.com"
	config.SuperAdminPassword = "first-password"
	config.SuperAdminResetPassword = false
	defer func() {
		config.SuperAdminEmail = ""
		config.SuperAdminPassword = ""
	}()

	if err := EnsureSuperAdmin(); err != nil {
		t.Fatal(err)
	}

	u, err := AuthenticateUser("root@example.com", "first-password")
	if err != nil {
		t.Fatalf("the super admin cannot log in: %s", err)
	}
	assertEqual(t, true, u.IsSuperAdmin, "is super admin")

	// the password is only set when the user is created
	config.SuperAdminPassword = "second-password"
	if err := EnsureSuperAdmin(); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticateUser("root@example.com", "first-password"); err != nil {
		t.Fatal("the password was changed without asking for a reset")
	}

	config.SuperAdminResetPassword = true
	if err := EnsureSuperAdmin(); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticateUser("root@example.com", "second-password"); err != nil {
		t.Fatal("the password was not reset")
	}
}
