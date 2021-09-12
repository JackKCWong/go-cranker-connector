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

// Connector connects to a set of crankers
type Connector struct {
	// ServiceName is registered to cranker to prefix the url under cranker. e.g. hello-world is accessible via /hello-world
	ServiceName string
	// ServiceURL is the root URL where the service is running
	ServiceURL string
	// WSSHttpClient is the cranker facing http client used for websocket connection
	WSSHttpClient *http.Client
	// ServiceHttpClient is the service facing http client used for servicing request/response
	ServiceHttpClient *http.Client
	// ShutdownTimeout is the grace period for shutdown. Any long running request/response isn't finish before timeout are cancelled.
	ShutdownTimeout time.Duration
	// RediscoveryInterval is the interval to run the Discoverer function to reconnect to crankers.
	// The Connector does a diff of the Discoverer result and current connections to decide if keep/add/remove.
	// A zero value means never rediscover beyond the first time.
	RediscoveryInterval time.Duration
	m                   sync.Mutex
	crankers            *sync.Map
	log                 zerolog.Logger
}

func (c *Connector) Connect(crankerDiscoverer Discoverer, slidingWindow int8) error {
	c.m.Lock()
	c.crankers = &sync.Map{}

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

	c.m.Unlock()
	crankerDiscoverChan := make(chan string, 10)
	go func() {
		// discovery
		for {
			urls := crankerDiscoverer()
			var latest map[string]bool = make(map[string]bool)
			for _, url := range urls {
				latest[url] = true
				_, exist := c.crankers.Load(url)
				if !exist {
					crankerDiscoverChan <- url
				}
			}

			c.crankers.Range(func(existing, wss interface{}) bool {
				if !latest[existing.(string)] {
					c.crankers.Delete(existing)
					go wss.(*core.WSSConnector).Shutdown()
				}

				return true
			})
			if c.RediscoveryInterval == 0 {
				close(crankerDiscoverChan)
				return
			} else {
				<-time.After(c.RediscoveryInterval)
				continue
			}
		}
	}()

	go func() {
		// connect and serve
		for url := range crankerDiscoverChan {
			wss := &core.WSSConnector{
				RegisterURL:       url,
				SlidingWindow:     slidingWindow,
				ServiceName:       c.ServiceName,
				ServiceURL:        c.ServiceURL,
				ShutdownTimeout:   c.ShutdownTimeout,
				WSSHttpClient:     c.WSSHttpClient,
				ServiceHttpClient: c.ServiceHttpClient,
			}

			c.crankers.Store(wss.RegisterURL, wss)

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
	}()

	c.log.Info().
		Msg("connector started")

	return nil
}

func (c *Connector) Shutdown() {
	c.m.Lock()
	defer c.m.Unlock()

	wg := &sync.WaitGroup{}
	c.crankers.Range(func(_, wss interface{}) bool {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wss.(*core.WSSConnector).Shutdown()
		}()
		return true
	})

	wg.Wait()
}
