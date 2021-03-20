package cranker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/JackKCWong/go-cranker-connector/pkg/config"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"nhooyr.io/websocket"
)

const markerReqBodyPending = "_1"
const markerReqHasNoBody = "_2"
const markerReqBodyEnded = "_3"

type Status int

const (
	NEW Status = iota
	STARTED
	STOPPED
)

// ConnectorSocket represents a connection to a cranker router
type ConnectorSocket struct {
	UUID            string
	routerURL       string
	serviceName     string
	servicePrefix   string
	serviceURL      string
	serviceFacingHC *http.Client
	crankerFacingHC *http.Client
	wss             *websocket.Conn
	buf             []byte
	log             zerolog.Logger
	mux             *sync.Mutex
	connectContext  context.Context
	cancelReconnect context.CancelFunc
	serviceContext  context.Context
	cancelService   context.CancelFunc
	status          Status
	chDone          chan int
}

// NewConnectorSocket returns one new connection to a router URL.
func NewConnectorSocket(routerURL, serviceName, serviceURL string,
	rc *config.RouterConfig, serviceFacingHC *http.Client) *ConnectorSocket {

	uuid := uuid.New().String()
	connCtx, cancelReconnect := context.WithCancel(context.Background())
	serviceCtx, cancelService := context.WithCancel(context.Background())

	return &ConnectorSocket{
		log: log.With().
			Str("socketId", uuid).
			Str("routerURL", routerURL).
			Str("serviceURL", serviceURL).
			Str("serviceName", serviceName).
			Logger(),

		UUID:            uuid,
		routerURL:       routerURL,
		serviceName:     serviceName,
		servicePrefix:   "/" + serviceName,
		serviceURL:      serviceURL,
		serviceFacingHC: serviceFacingHC,
		crankerFacingHC: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: rc.TLSClientConfig,
			}},
		buf:             make([]byte, 4*1024),
		mux:             &sync.Mutex{},
		connectContext:  connCtx,
		cancelReconnect: cancelReconnect,
		serviceContext:  serviceCtx,
		cancelService:   cancelService,
		chDone:          make(chan int, 1),
	}
}

// Close the underlying websocket connection
func (s *ConnectorSocket) Close(ctx context.Context) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.cancelReconnect()
	s.cancelService()

	select {
	case <-s.chDone:
		log.Info().Msg("closing gracefully")
	case <-ctx.Done():
		log.Info().Msg("close timeout. disconnecting forcefully...")
	}

	s.disconnect()

	return nil
}

func (s *ConnectorSocket) disconnect() error {
	s.status = STOPPED
	if s.wss != nil {
		s.log.Debug().
			Msg("socket connection closing")

		defer s.log.Debug().
			Msg("socket connection closed")
		// I was tempted to set s.wss to nil after Close here.
		// DON'T do it. It's better to read/write a closed connection and get an error
		// than to panic on a nil pointer
		return s.wss.Close(websocket.StatusNormalClosure, "close requested by client")
	}

	return nil
}

func (s *ConnectorSocket) dial() error {
	headers := http.Header{}
	headers.Add("CrankerProtocol", "1.0")
	headers.Add("Route", s.serviceName)

	backoff := 5000
	retryCount := 0
	for {
		select {
		case <-s.connectContext.Done():
			s.log.Debug().
				Msg("connect is cancelled")

			return errors.New("ConnectCancelledErr")

		default:
			retryCount++
			if retryCount > 1 {
				backoff = backoff * 2
				if backoff > 300000 {
					// spread the reconnect over 100s to avoid reconnection storm.
					backoff = 300000 + rand.Intn(100000)
				}
				s.log.Info().
					Int("retry", retryCount).
					Int("backoff", backoff).
					Msg("reconnecting to cranker")

				ctx, cancel := context.WithTimeout(s.connectContext, time.Duration(backoff)*time.Millisecond)
				<-ctx.Done()
				cancel()
				if s.connectContext.Err() != nil {
					// cancelled
					return ctx.Err()
				}
			}

			ctx, cancel := context.WithTimeout(s.connectContext, 30*time.Second)
			s.mux.Lock()
			conn, resp, err := websocket.Dial(
				ctx,
				fmt.Sprintf("%s/%s", s.routerURL, "register"),
				&websocket.DialOptions{
					HTTPClient: s.crankerFacingHC,
					HTTPHeader: headers,
				})

			cancel()

			s.wss = conn
			s.mux.Unlock()

			if err != nil {
				s.log.Error().
					Str("error", err.Error()).
					Msg("failed to connect to cranker router")

				continue
			} else if resp != nil {
				s.log.Debug().
					Str("status", resp.Status).
					Msg("wss connected")

				return nil
			} else {
				// timeout or cancelled
				continue
			}
		}
	}
}

// Connect connection to cranker router and consume incoming requests.
func (s *ConnectorSocket) Connect() error {
	s.mux.Lock()

	if s.status == STARTED {
		return errors.New("IllegalStatus: socket already started")
	}

	s.status = STARTED

	s.mux.Unlock()

	s.log.Info().Msg("socket starting")

	err := s.dial()
	if err != nil {
		s.log.Error().AnErr("err", err).Msg("error dialing")
		s.chDone <- 0
		return err
	}

	go func() {
		s.handleRequest()
		select {
		case <-s.connectContext.Done():
			s.log.Info().Msg("rejoin cancelled")
			s.chDone <- 0
		default:
			s.log.Info().Msg("rejoining...")
			s.Connect()
		}
	}()

	s.log.Info().Msg("socket started")

	return nil
}

func (s *ConnectorSocket) nextRequest() (*http.Request, error) {
	messageType, message, err := s.wss.Reader(s.connectContext)
	if err != nil {
		return nil, fmt.Errorf("RequestReaderError: %w", err)
	}

	if messageType != websocket.MessageText {
		s.log.Error().
			Str("expectedMessageType", "textMessage").
			Str("actualMessageType", "binaryMessage").
			Msg("protocal error")

		return nil, errors.New("CrankerProtoError: request not started with text message")
	}

	headerSize, err := message.Read(s.buf)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("RequestReadError: %w", err)
	}

	s.log.Debug().Bytes("recv", s.buf[0:headerSize]).Msg("wss msg received")

	firstline := s.buf[0:bytes.IndexByte(s.buf, '\n')]
	method, url := decomposeMethodAndURL(string(firstline))
	url = strings.TrimPrefix(url, s.servicePrefix)

	var req *http.Request
	marker := s.buf[headerSize-2 : headerSize]
	if bytes.Compare(marker, []byte(markerReqHasNoBody)) == 0 {
		s.log.Debug().Msg("request without body")
		req, err = http.NewRequestWithContext(s.serviceContext, method, url, nil)
	} else {
		s.log.Debug().Msg("request with body")
		r, w := io.Pipe()
		req, err = http.NewRequestWithContext(s.serviceContext, method, url, r)
		if err == nil {
			go s.pumpRequestBody(w)
		}
	}

	return req, err
}

func (s *ConnectorSocket) pumpRequestBody(out *io.PipeWriter) {
	for {
		s.log.Debug().Msg("draining request body")
		messageType, message, err := s.wss.Reader(s.serviceContext)
		if err != nil {
			s.log.Error().
				AnErr("err", err).
				Msg("failed to create reader for request body")
			return
		}

		switch messageType {
		case websocket.MessageBinary:
			n, err := io.CopyBuffer(out, message, s.buf)
			if err != nil {
				s.log.Error().
					AnErr("err", err).
					Msg("failed to send request body")
				return
			}

			s.log.Debug().Int64("bytesSent", n).Msg("sending request body")
		case websocket.MessageText:
			n, err := message.Read(s.buf)

			s.log.Debug().
				Bytes("recv", s.buf[0:n]).
				Msg("expecting a marker")

			if n == 2 {
				if bytes.Compare([]byte(markerReqBodyEnded), s.buf[0:2]) == 0 {
					out.Close()
					s.log.Debug().
						Msg("request ended")
					return
				}
			}

			s.log.Error().Bytes("recv", s.buf[0:n]).Msg("protocal error: not a marker")

			if err != nil || err != io.EOF {
				s.log.Error().AnErr("err", err).Msg("error reading marker")
				return
			}
		}
	}
}

func decomposeMethodAndURL(line string) (string, string) {
	parts := strings.Split(line, " ")
	return parts[0], parts[1]
}

func (s *ConnectorSocket) handleRequest() {
	defer s.disconnect()

	s.log.Info().
		Msg("waiting for request")

	req, err := s.nextRequest()
	if err != nil {
		s.log.Error().AnErr("readReqErr", err).Msg("error reading request")
		return
	}

	resp, err := s.sendRequest(req)
	if err != nil {
		errId := uuid.NewString()
		s.log.Error().
			AnErr("reqErr", err).
			Str("errorId", errId).
			Msg("error sending request")

		resp = &http.Response{
			Status:     "500 Server Error",
			StatusCode: 500,
			Body:       ioutil.NopCloser(bytes.NewBufferString(fmt.Sprintf("errorId=%s\n", errId))),
		}
	}

	err = s.sendResponse(resp)
	if err != nil {
		s.log.Error().AnErr("respErr", err).Msg("error sending response")
		return
	}
}

func (s *ConnectorSocket) sendRequest(req *http.Request) (*http.Response, error) {
	serviceURL, err := url.Parse(s.serviceURL)
	if err != nil {
		return nil, fmt.Errorf("InvalidServiceURLError: %w", err)
	}

	req.URL = serviceURL.ResolveReference(req.URL)
	req.RequestURI = ""

	s.log.Debug().
		Str("reqURL", req.URL.String()).
		Msg("prep req url")

	return s.serviceFacingHC.Do(req)
}

func (s *ConnectorSocket) sendResponse(resp *http.Response) error {
	defer resp.Body.Close()

	var headerBuf bytes.Buffer
	fmt.Fprintf(&headerBuf, "%s %s\r\n", resp.Proto, resp.Status)
	resp.Header.Write(&headerBuf)
	s.log.Debug().Bytes("respHeader", headerBuf.Bytes()).Msg("sending response headers")

	// write headers in text
	err := s.wss.Write(s.serviceContext, websocket.MessageText, headerBuf.Bytes())

	// write body in binary
	w, err := s.wss.Writer(s.serviceContext, websocket.MessageBinary)
	if err != nil {
		return fmt.Errorf("ResponseWriterError: %w", err)
	}

	defer w.Close()
	n, err := io.CopyBuffer(w, resp.Body, s.buf)

	if err != nil {
		return fmt.Errorf("ResponseWriteError: %w", err)
	}

	s.log.Debug().Int64("bytesSent", n).Msg("response sent")

	return nil
}
