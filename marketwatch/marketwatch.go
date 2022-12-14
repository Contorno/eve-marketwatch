package marketwatch

import (
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/contorno/eve-marketwatch/wsbroadcast"

	"github.com/contorno/goesi"
)

// MarketWatch provides CCP Market Data
type MarketWatch struct {
	// goesi client
	esi *goesi.APIClient

	// websocket handler
	broadcast *wsbroadcast.Hub

	// data store
	market    map[int64]*sync.Map
	contracts map[int64]*sync.Map
	mmutex    sync.RWMutex // Market mutex for the main map
	cmutex    sync.RWMutex // Contract mutex for the main map
}

// NewMarketWatch creates a new MarketWatch microservice
func NewMarketWatch() (*MarketWatch, error) {
	httpclient := &http.Client{
		Transport: &ApiTransport{
			next: &http.Transport{
				MaxIdleConns: 200,
				DialContext: (&net.Dialer{
					Timeout:   120 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				IdleConnTimeout:       3 * 60 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
				ExpectContinueTimeout: 0,
				MaxIdleConnsPerHost:   20,
			},
		},
	}

	return &MarketWatch{
		// ESI Client
		esi: goesi.NewAPIClient(
			httpclient,
			"eve-marketwatch (admin@eve.watch)",
		),

		// Websocket Broadcaster
		broadcast: wsbroadcast.NewHub([]string{"market", "contract"}),

		// Market Data Map
		market:    make(map[int64]*sync.Map),
		contracts: make(map[int64]*sync.Map),
	}, nil
}

// Run starts listening on port 3005 for API requests
func (s *MarketWatch) Run() error {

	// Set up the callback to send the market to the client on connect
	s.broadcast.OnRegister(s.dumpMarket)
	go s.broadcast.Run()

	go s.startUpMarketWorkers()

	// Handler for the websocket
	http.HandleFunc(
		"/",
		func(w http.ResponseWriter, r *http.Request) {
			err := s.broadcast.ServeWs(w, r)
			if err != nil {
				log.Println(err)
			}
		},
	)

	return http.ListenAndServe(":3005", nil)
}
