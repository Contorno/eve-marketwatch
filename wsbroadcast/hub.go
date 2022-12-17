package wsbroadcast

import (
	"log"
	"net/http"

	"github.com/getsentry/sentry-go"
	"github.com/gorilla/websocket"
)

// HandlerFunc is used for callbacks
// sends a list of channels the client registered to and a return channel
type HandlerFunc func(map[string]bool, chan interface{})

type fullMessage struct {
	Channel string
	Message interface{}
}

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Register requests from the clients.
	register chan *Client

	// Messages to the client
	broadcast chan fullMessage

	// Unregister requests from clients.
	unregister chan *Client

	// onRegister callbacks
	onRegister []HandlerFunc

	// which channels are available to register for
	channels []string
}

// NewHub Create a new hub for the handler
func NewHub(availableChannels []string) *Hub {
	hub := &Hub{
		broadcast:  make(chan fullMessage),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		channels:   availableChannels,
	}
	log.Printf("starting hub. available channels: %v\n", availableChannels)
	return hub
}

// Broadcast the message to clients
func (h *Hub) Broadcast(channel string, m interface{}) {
	h.broadcast <- fullMessage{channel, m}
}

// OnRegister calls a handler when a client registers.
func (h *Hub) OnRegister(f HandlerFunc) {
	h.onRegister = append(h.onRegister, f)
}

// Run the websocket handler
func (h *Hub) Run(localHub *sentry.Hub) {
	localHub.ConfigureScope(
		func(scope *sentry.Scope) {
			scope.SetTag("locationHash", "go#run-wsbroadcast-hub")
		},
	)

	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			for _, c := range h.onRegister {
				c(client.channels, client.send)
				log.Printf("registered %s to channels %v\n", client.conn.RemoteAddr(), client.channels)
			}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				log.Printf("unregistering %s\n", client.conn.RemoteAddr())
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				if client.CanSend(message.Channel) {
					client.send <- message.Message
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024 * 1024 * 500,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	EnableCompression: true,
}

// ServeWs handles websocket requests from the peer.
func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	// get a list of subscription requests
	channels := make(map[string]bool)
	for _, c := range h.channels {
		if r.URL.Query().Get(c) != "" {
			channels[c] = true
		}
	}

	// Create a new client
	client := &Client{
		hub:      h,
		conn:     conn,
		send:     make(chan interface{}, 256),
		channels: channels,
	}

	client.hub.register <- client
	go client.writePump(sentry.CurrentHub().Clone())
	go client.readPump(sentry.CurrentHub().Clone())

	return nil
}
