// Package websockets is used to broadcast messages to connected clients
package websockets

import (
	"encoding/json"
	"sync/atomic"

	"github.com/axllent/mailpit/internal/logger"
	"github.com/gorilla/websocket"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	Clients map[*Client]bool

	// Messages to broadcast to the clients.
	Broadcast chan broadcastMessage

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	// clientCount is an atomic count of connected clients, safe for concurrent reads.
	clientCount atomic.Int64
}

// AllProjects is the project ID used to broadcast to the clients of every project
const AllProjects = "*"

// broadcastMessage is a message for the clients of a single project
type broadcastMessage struct {
	project   string
	adminOnly bool
	data      []byte
}

// WebsocketNotification struct for responses
type WebsocketNotification struct {
	Type string
	Data any
}

// NewHub returns a new hub configuration
func NewHub() *Hub {
	return &Hub{
		Broadcast:  make(chan broadcastMessage),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		Clients:    make(map[*Client]bool),
	}
}

// Run runs the listener
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			if _, ok := h.Clients[client]; !ok {
				logger.Log().Debugf("[websocket] client %s connected", client.conn.RemoteAddr().String())
				h.Clients[client] = true
				h.clientCount.Add(1)
			}
		case client := <-h.unregister:
			if _, ok := h.Clients[client]; ok {
				logger.Log().Debugf("[websocket] client %s disconnected", client.conn.RemoteAddr().String())
				delete(h.Clients, client)
				close(client.send)
				h.clientCount.Add(-1)
			}
		case message := <-h.Broadcast:
			prepared, err := websocket.NewPreparedMessage(websocket.TextMessage, message.data)
			if err != nil {
				logger.Log().Errorf("[websocket] error preparing message: %s", err.Error())
				continue
			}
			for client := range h.Clients {
				if message.project != AllProjects && client.project != message.project {
					continue
				}
				if message.adminOnly && !client.admin {
					continue
				}
				select {
				case client.send <- prepared:
				default:
					close(client.send)
					delete(h.Clients, client)
					h.clientCount.Add(-1)
				}
			}
		}
	}
}

// ClientCount returns the number of connected clients
func (h *Hub) ClientCount() int64 {
	return h.clientCount.Load()
}

// Broadcast will spawn a broadcast message to all clients connected to the project.
// Use AllProjects to broadcast to every client, and "" in single mailbox mode.
func Broadcast(projectID, t string, msg any) {
	broadcast(broadcastMessage{project: projectID}, t, msg)
}

func broadcast(target broadcastMessage, t string, msg any) {
	if MessageHub == nil || MessageHub.clientCount.Load() == 0 {
		return
	}

	w := WebsocketNotification{}
	w.Type = t
	w.Data = msg
	b, err := json.Marshal(w)

	if err != nil {
		logger.Log().Errorf("[websocket] broadcast received invalid data: %s", err.Error())
		return
	}

	target.data = b

	go func() { MessageHub.Broadcast <- target }()
}

// BroadCastClientError is a wrapper to broadcast client errors to the web UI
func BroadCastClientError(severity, errorType, ip, message string) {
	msg := struct {
		Level   string
		Type    string
		IP      string
		Message string
	}{
		severity,
		errorType,
		ip,
		message,
	}

	// these contain information about every SMTP client, so are only for super admins
	broadcast(broadcastMessage{project: AllProjects, adminOnly: true}, "error", msg)
}
