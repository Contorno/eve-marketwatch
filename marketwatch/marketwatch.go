package marketwatch

import (
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/contorno/eve-marketwatch/wsbroadcast"

	"github.com/contorno/goesi"
	"golang.org/x/oauth2"
)

// MarketWatch provides CCP Market Data
type MarketWatch struct {
	// goesi client
	esi *goesi.APIClient

	// websocket handler
	broadcast *wsbroadcast.Hub

	// authentication
	doAuth    bool
	token     *oauth2.TokenSource
	tokenAuth *goesi.SSOAuthenticator

	// data store
	market     map[int64]*sync.Map
	structures map[int64]*Structure
	contracts  map[int64]*sync.Map
	mmutex     sync.RWMutex // Market mutex for the main map
	cmutex     sync.RWMutex // Contract mutex for the main map
	smutex     sync.RWMutex // Structure mutex for the whole map
}

// NewMarketWatch creates a new MarketWatch microservice
func NewMarketWatch(refresh, tokenClientID, tokenSecret string) *MarketWatch {
	httpclient := &http.Client{
		Transport: &ApiTransport{
			next: &http.Transport{
				MaxIdleConns: 200,
				DialContext: (&net.Dialer{
					Timeout:   300 * time.Second,
					KeepAlive: 5 * 60 * time.Second,
					DualStack: true,
				}).DialContext,
				IdleConnTimeout:       5 * 60 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
				ExpectContinueTimeout: 0,
				MaxIdleConnsPerHost:   20,
			},
		},
	}

	// Setup an authenticator for our user tokens
	doAuth := false
	if tokenClientID == "" || tokenSecret == "" || refresh == "" {
		log.Println("Warning: Missing authentication parameters so only regional market will be polled")
	} else {
		doAuth = true
	}
	auth := goesi.NewSSOAuthenticator(httpclient, tokenClientID, tokenSecret, "", []string{})

	tok := &oauth2.Token{
		Expiry:       time.Now(),
		AccessToken:  "",
		RefreshToken: refresh,
		TokenType:    "Bearer",
	}

	// Build our private token
	token := auth.TokenSource(tok)

	return &MarketWatch{
		// ESI Client
		esi: goesi.NewAPIClient(
			httpclient,
			"eve-marketwatch",
		),

		// Websocket Broadcaster
		broadcast: wsbroadcast.NewHub([]string{"market", "contract"}),

		// ESI SSO Handler
		doAuth:    doAuth,
		token:     &token,
		tokenAuth: auth,

		// Market Data Map
		market:     make(map[int64]*sync.Map),
		structures: make(map[int64]*Structure),
		contracts:  make(map[int64]*sync.Map),
	}
}

// Run starts listening on port 3005 for API requests
func (s *MarketWatch) Run() error {

	// Setup the callback to send the market to the client on connect
	s.broadcast.OnRegister(s.dumpMarket)
	go s.broadcast.Run()

	go s.startUpMarketWorkers()

	// Handler for the websocket
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.broadcast.ServeWs(w, r)
	})

	return http.ListenAndServe(":3005", nil)
}
