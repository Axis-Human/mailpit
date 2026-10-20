-- MULTI-PROJECT SUPPORT

-- every message & tag belongs to a project ('' for the single mailbox mode)
ALTER TABLE {{ tenant "mailbox" }} ADD COLUMN ProjectID TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS {{ tenant "idx_mailbox_project" }} ON {{ tenant "mailbox" }} (ProjectID, Created);

ALTER TABLE {{ tenant "tags" }} ADD COLUMN ProjectID TEXT NOT NULL DEFAULT '';
DROP INDEX IF EXISTS {{ tenant "idx_tag_name" }};
CREATE UNIQUE INDEX IF NOT EXISTS {{ tenant "idx_tag_project_name" }} ON {{ tenant "tags" }} (ProjectID, Name);

CREATE TABLE IF NOT EXISTS {{ tenant "projects" }} (
	ID TEXT NOT NULL PRIMARY KEY,
	Slug TEXT NOT NULL COLLATE NOCASE,
	Name TEXT NOT NULL,
	KeyHash TEXT NOT NULL,
	KeyHint TEXT NOT NULL DEFAULT '',
	Disabled INTEGER NOT NULL DEFAULT 0,
	Created INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS {{ tenant "idx_projects_slug" }} ON {{ tenant "projects" }} (Slug);
CREATE UNIQUE INDEX IF NOT EXISTS {{ tenant "idx_projects_keyhash" }} ON {{ tenant "projects" }} (KeyHash);

CREATE TABLE IF NOT EXISTS {{ tenant "users" }} (
	ID TEXT NOT NULL PRIMARY KEY,
	Email TEXT NOT NULL COLLATE NOCASE,
	Name TEXT NOT NULL DEFAULT '',
	PasswordHash TEXT NOT NULL,
	IsSuperAdmin INTEGER NOT NULL DEFAULT 0,
	Disabled INTEGER NOT NULL DEFAULT 0,
	Created INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS {{ tenant "idx_users_email" }} ON {{ tenant "users" }} (Email);

CREATE TABLE IF NOT EXISTS {{ tenant "project_users" }} (
	ProjectID TEXT NOT NULL,
	UserID TEXT NOT NULL,
	PRIMARY KEY (ProjectID, UserID)
);

CREATE INDEX IF NOT EXISTS {{ tenant "idx_project_users_user" }} ON {{ tenant "project_users" }} (UserID);

CREATE TABLE IF NOT EXISTS {{ tenant "sessions" }} (
	TokenHash TEXT NOT NULL PRIMARY KEY,
	UserID TEXT NOT NULL,
	Created INTEGER NOT NULL,
	Expires INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS {{ tenant "idx_sessions_user" }} ON {{ tenant "sessions" }} (UserID);
