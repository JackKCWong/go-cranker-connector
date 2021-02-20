package connector

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Connector connects the local service to crankers
type Connector struct {
	routerURLs []string
	targetURL  string
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
	c.routerURLs = routerURLs
	c.targetURL = serviceURL
	for i := 0; i < len(routerURLs); i++ {
		headers := http.Header{}
		headers.Add("CrankerProtocol", "1.0")
		headers.Add("Route", serviceName)
		conn, resp, err := c.dialer.Dial(
			fmt.Sprintf("%s/%s", routerURLs[i], "register"),
			headers)

		if resp != nil {
			log.Debug().
				Str("status", resp.Status).
				Send()
		}

		if err != nil {
			log.Error().
				Str("router", routerURLs[i]).
				Str("error", err.Error()).
				Msg("failed to connect to cranker router")

			return err
		}

		cs := connectorSocket{
			routerURL:  routerURLs[i],
			targetURL:  serviceURL,
			wss:        conn,
			httpClient: c.httpClient,
		}

		go cs.start()
	}

	return nil
}

// Destroy stops and clean up all sockets
func (c *Connector) Destroy() error {
	return nil
}
