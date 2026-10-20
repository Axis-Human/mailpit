package storage

import (
	"context"

	"github.com/leporo/sqlf"
)

// AllProjects is a special project ID that disables project filtering. It is used for
// global operations such as statistics, the dump command and the Prometheus metrics.
// All other project IDs are generated, so they can never collide with it.
const AllProjects = "*"

// scopeProject restricts a query to a single project, unless projectID is AllProjects.
// The column should be qualified when the query uses an alias or joins other tables.
func scopeProject(q *sqlf.Stmt, column, projectID string) *sqlf.Stmt {
	if projectID == AllProjects {
		return q
	}

	return q.Where(column+" = ?", projectID)
}

// messageInProject returns whether a message exists in the project
func messageInProject(projectID, id string) bool {
	var total float64 // use float64 for rqlite compatibility

	q := sqlf.From(tenant("mailbox")).
		Select("COUNT(*)").To(&total).
		Where("ID = ?", id)

	if err := scopeProject(q, "ProjectID", projectID).QueryRowAndClose(context.TODO(), db); err != nil {
		return false
	}

	return total > 0
}

// MessageProjectID returns the ID of the project a message belongs to,
// the boolean is false if the message does not exist.
func MessageProjectID(id string) (string, bool) {
	var projectID string

	err := sqlf.From(tenant("mailbox")).
		Select("ProjectID").To(&projectID).
		Where("ID = ?", id).
		QueryRowAndClose(context.TODO(), db)
	if err != nil {
		return "", false
	}

	return projectID, true
}

// projectQuery appends a project restriction to a raw SQL query that already has a WHERE clause.
// The project ID argument must be appended last, see projectArgs.
func projectQuery(query, projectID string) string {
	if projectID == AllProjects {
		return query
	}

	return query + " AND ProjectID = ?"
}

// projectArgs appends the project ID argument required by projectQuery
func projectArgs(args []any, projectID string) []any {
	if projectID == AllProjects {
		return args
	}

	return append(args[:len(args):len(args)], projectID)
}
