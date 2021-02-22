package connector

import (
	"crypto/tls"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Connector connects the local service to crankers
type Connector struct {
	routerURLs []string
	targetURL  string
	dialer     *websocket.Dialer
	httpClient *http.Client
	wgSockets  sync.WaitGroup
}

// NewConnector returns a new Connector
func NewConnector() *Connector {
	return &Connector{
		dialer: &websocket.Dialer{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// NewConnectorWithConfig creates a Connector with custom config
func NewConnectorWithConfig(tlsConfig *tls.Config) *Connector {
	return &Connector{
		dialer: &websocket.Dialer{
			TLSClientConfig: tlsConfig,
		},
		httpClient: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}},
	}
}

// Connect to the target crankers
func (c *Connector) Connect(
	routerURLs []string, slidingWindow int,
	serviceName string, serviceURL string) error {
	c.routerURLs = routerURLs
	c.targetURL = serviceURL
	for i := 0; i < len(routerURLs); i++ {
		for j := 0; j < slidingWindow; j++ {
			cs := connectorSocket{
				routerURL:   routerURLs[i],
				targetURL:   serviceURL,
				httpClient:  c.httpClient,
				dialer:      c.dialer,
				serviceName: serviceName,
			}

			c.wgSockets.Add(1)
			go func() {
				defer c.wgSockets.Done()
				cs.start()
			}()
		}
	}

	c.wgSockets.Wait()

	return nil
}

// Destroy stops and clean up all sockets
func (c *Connector) Destroy() error {
	defer log.Info().Msg("connector destroyed")
	log.Info().Msg("destroying connector")

	return nil
}
