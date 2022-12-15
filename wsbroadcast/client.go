package wsbroadcast

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 60 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

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
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		err := c.conn.Close()
		if err != nil {
			log.Println(err)
		}
	}()
	c.conn.SetReadLimit(1)
	err := c.conn.SetReadDeadline(time.Now().Add(pongWait))
	if err != nil {
		log.Println(err)
	}

	c.conn.SetPongHandler(
		func(string) error {
			err := c.conn.SetReadDeadline(time.Now().Add(pongWait))
			if err != nil {
				return err
			}
			return nil
		},
	)

	for {
		// /dev/null
		_, _, err := c.conn.ReadMessage()
		if err != nil {
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
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		err := c.conn.Close()
		if err != nil {
			log.Println(err)
		}
	}()

	for {
		select {
		case message, ok := <-c.send:
			err := c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err != nil {
				log.Println(err)
			}

			if !ok {
				// The hub closed the channel.
				err = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				if err != nil {
					log.Println(err)
				}
				return
			}

			// Write the object out
			err = c.conn.WriteJSON(message)
			if err != nil {
				log.Println(err)
				return
			}

		case <-ticker.C:
			err := c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err != nil {
				log.Println(err)
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Println(err)
				return
			}
		}
	}
}
