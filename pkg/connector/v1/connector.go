package connector

import (
	"context"
	"net/http"
	"net/url"
	"sync"

	"github.com/JackKCWong/go-cranker-connector/internal/cranker"
	"github.com/JackKCWong/go-cranker-connector/pkg/config"
	"github.com/rs/zerolog/log"
)

// Connector connects the local service to crankers
type Connector struct {
	routerURLs       []*url.URL
	serviceURL       *url.URL
	serviceFacingHC  *http.Client
	routerConfig     *config.RouterConfig
	connectorSockets []*cranker.ConnectorSocket
	mux              *sync.Mutex
}

// NewConnector returns a new Connector
func NewConnector(rc *config.RouterConfig, sc *config.ServiceConfig) *Connector {
	return &Connector{
		routerConfig: rc,
		serviceFacingHC: sc.HTTPClient,
		mux:             &sync.Mutex{},
	}
}

// Connect to the target crankers
func (c *Connector) Connect(
	routerURLs []string, slidingWindow int,
	serviceName string, serviceURL string) error {

	c.mux.Lock()

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

	c.connectorSockets = make([]*cranker.ConnectorSocket, 0, noOfRouterURLs*slidingWindow)
	var wgSockets sync.WaitGroup

	for i := 0; i < noOfRouterURLs; i++ {
		for j := 0; j < slidingWindow; j++ {
			cs := cranker.NewConnectorSocket(
				c.routerURLs[i].String(),
				serviceName,
				serviceURL,
				c.routerConfig,
				c.serviceFacingHC)

			wgSockets.Add(1)
			c.connectorSockets = append(c.connectorSockets, cs)
			go func() {
				defer wgSockets.Done()
				cs.Connect()
			}()
		}
	}

	c.mux.Unlock() // unlock before wait to allow Shutdown with cancel / timeout

	wgSockets.Wait()

	return nil
}

// Shutdown stops and clean up all sockets
func (c *Connector) Shutdown(ctx context.Context) {
	defer log.Info().Msg("connector destroyed")

	log.Info().
		Int("sockets", len(c.connectorSockets)).
		Msg("destroying connector")

	c.mux.Lock()
	defer c.mux.Unlock()

	var wg sync.WaitGroup
	for _, s := range c.connectorSockets {
		wg.Add(1)
		go func(s *cranker.ConnectorSocket) {
			defer wg.Done()
			close(ctx, s)
		}(s)
	}

	wg.Wait()
}

func close(ctx context.Context, s *cranker.ConnectorSocket) {
	log.Info().Str("socketId", s.UUID).Msg("socket closing")
	s.Close(ctx)
	log.Info().Str("socketId", s.UUID).Msg("socket closed")
}
