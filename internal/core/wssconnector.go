package core

import (
	"context"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
	"net/http"
	"nhooyr.io/websocket"
	"sync"
	"time"
)

var rawBuffers *sync.Pool = &sync.Pool{
	New: func() interface{} {
		return make([]byte, 8*1024)
	},
}

// WSSConnector connects to a single cranker wss url.
type WSSConnector struct {
	ServiceName       string
	ServiceURL        string
	RegisterURL       string
	SlidingWindow     int8
	ShutdownTimeout   time.Duration
	WSSHttpClient     *http.Client
	ServiceHttpClient *http.Client
	terminate         context.CancelFunc
	wg                sync.WaitGroup
	connections       map[string]*websocket.Conn
	log               zerolog.Logger
}


// ConnectAndServe blocks until the *WSSConnector.Shutdown() is called.
func (wss *WSSConnector) ConnectAndServe() error {
	wss.log = log.With().
		Str("serviceURL", wss.ServiceURL).
		Str("serviceName", wss.ServiceName).
		Str("registerURL", wss.RegisterURL).
		Logger()

	wss.log.Info().Msg("ConnectAndServe starting")

	var sem *semaphore.Weighted = semaphore.NewWeighted(int64(wss.SlidingWindow))
	sigTerm, terminate := context.WithCancel(context.Background())
	defer terminate()
	wss.terminate = terminate
	var err error

	for {
		select {
		case <-sigTerm.Done():
			wss.log.Info().Msg("terminating...")
			return sigTerm.Err()
		default:
			err = sem.Acquire(sigTerm, 1)
			if err != nil {
				return err
			}

			wss.wg.Add(1)
			go func() {
				defer wss.wg.Done()

				var s *WssWorker = &WssWorker{
					ServiceName:     wss.ServiceName,
					RegisterURL:     wss.RegisterURL,
					ServiceURL:      wss.ServiceURL,
					ShutdownTimeout: wss.ShutdownTimeout,
				}

				err = s.Dial(sigTerm, wss.WSSHttpClient)
				if err != nil {
					return
				}

				buf := rawBuffers.Get().([]byte)
				defer rawBuffers.Put(buf)
				err = s.Serve(sigTerm, sem, wss.ServiceHttpClient, buf)
				if err != nil {
					return
				}
			}()
		}
	}
}

func (wss *WSSConnector) Shutdown() {
	wss.log.Info().Msg("shutting down")
	wss.terminate()
	wss.wg.Wait()
}
