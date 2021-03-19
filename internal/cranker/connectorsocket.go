package cranker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	uuid            string
	routerURL       string
	serviceName     string
	servicePrefix   string
	serviceURL      string
	httpClient      *http.Client
	wss             *websocket.Conn
	buf             []byte
	log             zerolog.Logger
	mux             *sync.Mutex
	connectContext  context.Context
	cancelReconnect context.CancelFunc
	serviceContext  context.Context
	cancelService   context.CancelFunc
	status          Status
}

// NewConnectorSocket returns one new connection to a router URL.
func NewConnectorSocket(routerURL, serviceName, serviceURL string,
	config *config.RouterConfig, httpClient *http.Client) *ConnectorSocket {

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

		uuid:            uuid,
		routerURL:       routerURL,
		serviceName:     serviceName,
		servicePrefix:   "/" + serviceName,
		serviceURL:      serviceURL,
		httpClient:      httpClient,
		buf:             make([]byte, 4*1024),
		mux:             &sync.Mutex{},
		connectContext:  connCtx,
		cancelReconnect: cancelReconnect,
		serviceContext:  serviceCtx,
		cancelService:   cancelService,
	}
}

// Close the underlying websocket connection
func (s *ConnectorSocket) Close(ctx context.Context) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.cancelReconnect()
	<-ctx.Done()
	s.cancelService()

	return nil
}

func (s *ConnectorSocket) disconnect() error {
	s.status = STOPPED
	if s.wss != nil {
		s.log.Debug().
			Msg("closing socket connection")

		// I was tempted to set s.wss to nil after Close here.
		// DON'T do it. It's better to read/write a closed connection and get an error
		// than to panic on a nil pointer
		return s.wss.Close(websocket.StatusNormalClosure, "close requested by client")
	}

	return nil
}

func (s *ConnectorSocket) dial(parent context.Context) error {
	headers := http.Header{}
	headers.Add("CrankerProtocol", "1.0")
	headers.Add("Route", s.serviceName)

	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(
		ctx,
		fmt.Sprintf("%s/%s", s.routerURL, "register"),
		&websocket.DialOptions{
			HTTPClient: s.httpClient,
			HTTPHeader: headers,
		})

	if resp != nil {
		s.log.Debug().
			Str("status", resp.Status).
			Msg("wss connected")
	}

	if ctx.Err() != nil {
		s.log.Debug().
			AnErr("reason", ctx.Err()).Msg("connect is cancelled or timeout")

		return ctx.Err()
	}

	if err != nil {
		s.log.Error().
			Str("error", err.Error()).
			Msg("failed to connect to cranker router")

		return err
	}

	s.wss = conn

	return nil
}

// Connect connection to cranker router and consume incoming requests.
func (s *ConnectorSocket) Connect() error {
	s.mux.Lock()
	defer s.mux.Unlock()

	if s.status == STARTED {
		return errors.New("IllegalStatus: socket already started")
	}

	s.status = STARTED

	s.log.Info().Msg("socket starting")

	err := s.dial(s.connectContext)
	if err != nil {
		s.log.Error().AnErr("err", err).Msg("error dialing")
		return err
	}

	go func() {
		s.handleRequest(s.serviceContext)
		select {
		case <-s.connectContext.Done():
			s.log.Info().Msg("reconnect cancelled")
		default:
			s.log.Info().Msg("reconnecting")
			s.Connect()
		}
	}()

	s.log.Info().Msg("socket started")

	return nil
}

func (s *ConnectorSocket) nextRequest(ctx context.Context) (*http.Request, error) {
	messageType, message, err := s.wss.Reader(ctx)
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
		req, err = http.NewRequest(method, url, nil)
	} else {
		s.log.Debug().Msg("request with body")
		r, w := io.Pipe()
		req, err = http.NewRequest(method, url, r)
		if err == nil {
			go s.pumpRequestBody(ctx, w)
		}
	}

	return req, err
}

func (s *ConnectorSocket) pumpRequestBody(ctx context.Context, out *io.PipeWriter) {
	for {
		s.log.Debug().Msg("draining request body")
		messageType, message, err := s.wss.Reader(ctx)
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

func (s *ConnectorSocket) handleRequest(ctx context.Context) {
	defer s.disconnect()

	s.log.Info().
		Msg("waiting for request")

	req, err := s.nextRequest(ctx)
	if err != nil {
		s.log.Error().AnErr("readReqErr", err).Msg("error reading request")
		return
	}

	resp, err := s.sendRequest(ctx, req)
	if err != nil {
		s.log.Error().AnErr("reqErr", err).Msg("error sending request")
		return
	}

	err = s.sendResponse(ctx, resp)
	if err != nil {
		s.log.Error().AnErr("respErr", err).Msg("error sending response")
		return
	}
}

func (s *ConnectorSocket) sendRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	serviceURL, err := url.Parse(s.serviceURL)
	if err != nil {
		return nil, fmt.Errorf("InvalidServiceURLError: %w", err)
	}

	req.URL = serviceURL.ResolveReference(req.URL)
	req.RequestURI = ""

	s.log.Debug().
		Str("reqURL", req.URL.String()).
		Msg("prep req url")

	return s.httpClient.Do(req)
}

func (s *ConnectorSocket) sendResponse(ctx context.Context, resp *http.Response) error {
	defer resp.Body.Close()

	var headerBuf bytes.Buffer
	fmt.Fprintf(&headerBuf, "%s %s\r\n", resp.Proto, resp.Status)
	resp.Header.Write(&headerBuf)
	s.log.Debug().Bytes("respHeader", headerBuf.Bytes()).Msg("sending response headers")

	// write headers in text
	err := s.wss.Write(ctx, websocket.MessageText, headerBuf.Bytes())

	// write body in binary
	w, err := s.wss.Writer(ctx, websocket.MessageBinary)
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
