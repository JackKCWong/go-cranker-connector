package connector

import (
	"net/http"
	"net/url"
	"sync"

	"github.com/go-cranker/internal/cranker"
	"github.com/go-cranker/pkg/config"
	"github.com/rs/zerolog/log"
)

// Connector connects the local service to crankers
type Connector struct {
	routerURLs       []*url.URL
	serviceURL       *url.URL
	httpClient       *http.Client
	routerConfig     *config.RouterConfig
	connectorSockets []*cranker.ConnectorSocket
}

// NewConnector returns a new Connector
// NewConnectorWithConfig creates a Connector with custom config
func NewConnector(rc *config.RouterConfig, sc *config.ServiceConfig) *Connector {
	return &Connector{
		routerConfig: rc,
		httpClient:   sc.HTTPClient,
	}
}

// Connect to the target crankers
func (c *Connector) Connect(
	routerURLs []string, slidingWindow int,
	serviceName string, serviceURL string) error {

	var err error
	c.serviceURL, err = url.Parse(serviceURL)

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

	c.connectorSockets = make([]*cranker.ConnectorSocket,0, noOfRouterURLs*slidingWindow)

	for i := 0; i < noOfRouterURLs; i++ {
		for j := 0; j < slidingWindow; j++ {
			cs := cranker.NewConnectorSocket(
				c.routerURLs[i].String(),
				serviceName,
				c.serviceURL.String(),
				c.routerConfig,
				c.httpClient)

			wgSockets.Add(1)
			c.connectorSockets = append(c.connectorSockets, cs)
			go func() {
				defer wgSockets.Done()
				cs.Connect()
			}()
		}
	}

	wgSockets.Wait()

	return nil
}

// Shutdown stops and clean up all sockets
func (c *Connector) Shutdown() error {
	defer log.Info().Msg("connector destroyed")

	log.Info().
		Int("sockets", len(c.connectorSockets)).
		Msg("destroying connector")

	for _, cs := range c.connectorSockets {
		log.Debug().Interface("socket", cs).Msg("closing connector socket")
		cs.Close()
	}

	return nil
}
