package wsbroadcast

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestWebSocket(t *testing.T) {
	// Set up a new hub
	hub := NewHub([]string{"market"})

	hub.OnRegister(
		func(subs map[string]bool, send chan interface{}) {
			send <- "sup"
		},
	)

	go hub.Run()

	// Run a webserver for the socket
	http.HandleFunc(
		"/", func(w http.ResponseWriter, r *http.Request) {
			err := hub.ServeWs(w, r)
			if err != nil {
				log.Fatal(err)
			}
		},
	)

	// Listen on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.Nil(t, err)

	// Get the address
	addr := listener.Addr()
	go func() { panic(http.Serve(listener, nil)) }()

	// Set up a client for testing
	u := url.URL{Scheme: "ws", Host: addr.String(), Path: "/"}

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	assert.Nil(t, err)
	message := ""
	err = c.ReadJSON(&message)
	assert.Nil(t, err)

	assert.Equal(t, "sup", message)

	err = c.Close()
	assert.Nil(t, err)
}
