package connector

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Connector connects the local service to crankers
type Connector struct {
	routerURLs []*url.URL
	targetURL  *url.URL  
	dialer     *websocket.Dialer
	httpClient *http.Client
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

	var err error
	c.targetURL, err = url.Parse(serviceURL)

	if err != nil {
		return err
	}

	noOfRouterURLs := len(routerURLs)
	c.routerURLs = make([]*url.URL, noOfRouterURLs)
	for i := 0; i < noOfRouterURLs; i++ {
		c.routerURLs[i], err = url.Parse(routerURLs[i])
		if err != nil {
			return err
		}
	}

	var wgSockets sync.WaitGroup

	for i := 0; i < noOfRouterURLs; i++ {
		for j := 0; j < slidingWindow; j++ {
			cs := connectorSocket{
				routerURL:   c.routerURLs[i].String(),
				targetURL:   c.targetURL.String(),
				httpClient:  c.httpClient,
				dialer:      c.dialer,
				serviceName: serviceName,
				buf: make([]byte, 16 * 1024),
			}

			wgSockets.Add(1)
			go func() {
				defer wgSockets.Done()
				cs.start()
			}()
		}
	}

	wgSockets.Wait()

	return nil
}

// Shutdown stops and clean up all sockets
func (c *Connector) Shutdown() error {
	defer log.Info().Msg("connector destroyed")
	log.Info().Msg("destroying connector")

	return nil
}
