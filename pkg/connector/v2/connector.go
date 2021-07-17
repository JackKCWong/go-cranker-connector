package connector

import (
	"context"
	"errors"
	"github.com/JackKCWong/go-cranker-connector/internal/core"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"net/http"
	"sync"
	"time"
)

type Discoverer func() []string

type Connector struct {
	m                 sync.Mutex
	ServiceName       string
	ServiceURL        string
	WSSHttpClient     *http.Client
	ServiceHttpClient *http.Client
	ShutdownTimeout   time.Duration
	children          map[string]*core.WSSConnector
	log               zerolog.Logger
}

func (c *Connector) Connect(crankerDiscovery Discoverer, slidingWindow int8) error {
	c.m.Lock()
	defer c.m.Unlock()

	c.children = make(map[string]*core.WSSConnector)

	if c.ServiceURL == "" {
		return errors.New("requires ServiceURL")
	}

	if c.ServiceName == "" {
		return errors.New("requires ServiceName")
	}

	if c.WSSHttpClient == nil {
		c.WSSHttpClient = http.DefaultClient
	}

	if c.ServiceHttpClient == nil {
		c.ServiceHttpClient = http.DefaultClient
	}

	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 5 * time.Second
	}

	if slidingWindow <= 0 {
		return errors.New("slidingWindow must be greater than 0")
	}

	c.log = log.With().
		Str("serviceURL", c.ServiceURL).
		Str("serviceName", c.ServiceName).
		Logger()

	for _, url := range crankerDiscovery() {
		wss := &core.WSSConnector{
			RegisterURL:       url,
			SlidingWindow:     slidingWindow,
			ServiceName:       c.ServiceName,
			ServiceURL:        c.ServiceURL,
			ShutdownTimeout:   c.ShutdownTimeout,
			WSSHttpClient:     c.WSSHttpClient,
			ServiceHttpClient: c.ServiceHttpClient,
		}

		c.children[wss.RegisterURL] = wss

		go func() {
			err := wss.ConnectAndServe()
			if err != nil {
				switch err {
				case context.Canceled:
					c.log.Info().
						Str("crankerWSS", wss.RegisterURL).
						Msg("wss connector exiting gracefully")
				case context.DeadlineExceeded:
					c.log.Info().
						Str("crankerWSS", wss.RegisterURL).
						Msg("wss connector exiting forcefully")
				}

				return
			}
		}()
	}

	c.log.Info().
		Msg("connector started")

	return nil
}

func (c *Connector) Shutdown() {
	c.m.Lock()
	defer c.m.Unlock()

	wg := &sync.WaitGroup{}
	for _, wss := range c.children {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wss.Shutdown()
		}()
	}

	wg.Wait()
}
