package storage

import (
	"sync"
	"time"

	"github.com/axllent/mailpit/config"
	"github.com/axllent/mailpit/server/websockets"
)

var (
	// bcStatsPending holds the projects which have a stats broadcast pending
	bcStatsPending = map[string]bool{}
	bcStatsMutex   sync.Mutex
)

// BroadcastMailboxStats broadcasts the total number of messages
// displayed to the web UI, as well as the total unread messages.
// The lookup is very fast (< 10ms / 100k messages under load).
// Rate limited to 4x per second per project.
func BroadcastMailboxStats(projectID string) {
	// only one broadcast may be pending at a time for each project
	bcStatsMutex.Lock()
	if bcStatsPending[projectID] {
		bcStatsMutex.Unlock()
		return
	}
	bcStatsPending[projectID] = true
	bcStatsMutex.Unlock()

	go func() {
		time.Sleep(250 * time.Millisecond)

		bcStatsMutex.Lock()
		delete(bcStatsPending, projectID)
		bcStatsMutex.Unlock()

		b := struct {
			Total   uint64
			Unread  uint64
			Version string
		}{
			Total:   CountTotal(projectID),
			Unread:  CountUnread(projectID),
			Version: config.Version,
		}

		websockets.Broadcast(projectID, "stats", b)
	}()
}
