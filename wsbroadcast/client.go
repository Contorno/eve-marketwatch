package wsbroadcast

import (
	"log"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gorilla/websocket"
)

var zeroTime time.Time

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan interface{}

	// Channels available to the client
	channels map[string]bool
}

// CanSend checks if the client is subscribed to a channel
func (c *Client) CanSend(channel string) bool {
	return c.channels[channel]
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump(localHub *sentry.Hub) {
	localHub.ConfigureScope(
		func(scope *sentry.Scope) {
			scope.SetTag("locationHash", "go#read-pump")
		},
	)

	defer func() {
		c.hub.unregister <- c
		err := c.conn.Close()
		if err != nil {
			sentry.CaptureException(err)
			log.Println(err)
		}
	}()
	c.conn.SetReadLimit(1)
	err := c.conn.SetReadDeadline(zeroTime)
	if err != nil {
		sentry.CaptureException(err)
		log.Println(err)
	}

	for {
		// /dev/null
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			sentry.CaptureException(err)
			log.Println(err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump(localHub *sentry.Hub) {
	localHub.ConfigureScope(
		func(scope *sentry.Scope) {
			scope.SetTag("locationHash", "go#write-pump")
		},
	)

	defer func() {
		err := c.conn.Close()
		if err != nil {
			sentry.CaptureException(err)
			log.Println(err)
		}
	}()

	for {
		message, ok := <-c.send

		err := c.conn.SetWriteDeadline(zeroTime)
		if err != nil {
			sentry.CaptureException(err)
			log.Println(err)
		}

		if !ok {
			err = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			if err != nil {
				sentry.CaptureException(err)
				log.Println(err)
			}
			return
		}

		err = c.conn.WriteJSON(message)
		if err != nil {
			sentry.CaptureException(err)
			log.Println(err)
			return
		}
	}
}
