package cranker

import (
	"bufio"
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
	"sync/atomic"
	"time"

	"github.com/JackKCWong/go-cranker-connector/internal/util"
	"github.com/JackKCWong/go-cranker-connector/pkg/config"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"nhooyr.io/websocket"
)

const markerReqBodyPending = "_1"
const markerReqHasNoBody = "_2"
const markerReqBodyEnded = "_3"

const (
	NEW int32 = iota
	STARTED
	STOPPED
)

type SigChan chan struct{}

// ConnectorSocket represents a connection to a cranker router
type ConnectorSocket struct {
	UUID            string
	routerURL       string
	serviceName     string
	servicePrefix   string
	serviceURL      string
	serviceFacingHC *http.Client
	crankerFacingHC *http.Client
	buffers         *sync.Pool
	log             zerolog.Logger
	sigTERM         *util.Flare
	sigKILL         *util.Flare
	sigDONE         *util.Flare
	status          int32
	redialLock      sync.Mutex
}

// NewConnectorSocket returns one new connection to a router URL.
func NewConnectorSocket(routerURL, serviceName, serviceURL string,
	rc *config.RouterConfig, serviceFacingHC *http.Client) *ConnectorSocket {

	uid := uuid.New().String()
	bufsize := 8 * 1024
	if rc.BufferSize > bufsize {
		bufsize = rc.BufferSize
	}

	return &ConnectorSocket{
		log: log.With().
			Str("socketId", uid).
			Str("routerURL", routerURL).
			Str("serviceURL", serviceURL).
			Str("serviceName", serviceName).
			Logger(),

		UUID:            uid,
		routerURL:       routerURL,
		serviceName:     serviceName,
		servicePrefix:   "/" + serviceName,
		serviceURL:      serviceURL,
		serviceFacingHC: serviceFacingHC,
		crankerFacingHC: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: rc.TLSClientConfig,
			}},
		buffers: &sync.Pool{
			New: func() interface{} {
				return make([]byte, bufsize)
			},
		},
		sigTERM:    util.NewFlare(),
		sigKILL:    util.NewFlare(),
		sigDONE:    util.NewFlare(),
		status:     NEW,
		redialLock: sync.Mutex{},
	}
}

// Close the underlying websocket connection
func (s *ConnectorSocket) Close(ctx context.Context) error {
	s.sigTERM.Fire()

	select {
	case <-s.sigDONE.Flashed():
		log.Info().Msg("terminating gracefully")
	case <-ctx.Done():
		log.Info().Msg("close timeout. disconnecting forcefully...")
	}

	s.sigKILL.Fire()

	return nil
}

func (s *ConnectorSocket) dial(parent context.Context) *websocket.Conn {
	s.redialLock.Lock()
	s.log.Info().Msg("dialing")
	headers := http.Header{}
	headers.Add("CrankerProtocol", "1.0")
	headers.Add("Route", s.serviceName)

	backoff := 5000
	retryCount := 0
	for {
		select {
		case <-s.sigTERM.Flashed():
			s.log.Debug().
				Msg("reconnect is terminated")

			return nil

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

				backoffCtx, cancelBackoff := context.WithTimeout(parent, time.Duration(backoff)*time.Millisecond)
				select {
				case <-backoffCtx.Done():
					cancelBackoff()
					break
				case <-parent.Done():
					cancelBackoff()
					return nil
				}
			}

			dialCtx, cancelDial := context.WithTimeout(parent, 30*time.Second)
			conn, resp, err := websocket.Dial(
				dialCtx,
				fmt.Sprintf("%s/%s", s.routerURL, "register"),
				&websocket.DialOptions{
					HTTPClient: s.crankerFacingHC,
					HTTPHeader: headers,
				})

			cancelDial()

			if err != nil {
				s.log.Error().
					Str("error", err.Error()).
					Msg("failed to connect to cranker router")

			} else if resp != nil {
				s.log.Info().
					Str("status", resp.Status).
					Msg("wss connected")

				backoff = 5000
				retryCount = 0
				return conn
			}
		}
	}
}

// Connect connection to cranker router and consume incoming requests.
func (s *ConnectorSocket) Connect() error {

	if !atomic.CompareAndSwapInt32(&s.status, s.status, STARTED) {
		s.log.Error().Msg("socket already started.")
		return errors.New("IllegalStatus: socket already started")
	}

	s.log.Info().Msg("socket starting")

	ctx, cancel := context.WithCancel(context.Background())
	chConn := make(chan *websocket.Conn)

	go func() {
		for {
			select {
			case <-ctx.Done():
				s.log.Info().Msg("stop redial")
			default:
				chConn <- s.dial(ctx)
			}
		}
	}()

	go func() {
		for conn := range chConn {
			s.proxyRequest(ctx, conn)
		}

		s.sigDONE.Fire()
		cancel() // just for clean up
		atomic.CompareAndSwapInt32(&s.status, s.status, STOPPED)
	}()

	go func() {
		// watch kill signal
		<-s.sigKILL.Flashed()
		s.log.Debug().Msg("killing")
		cancel() // cancelling in-flights
	}()

	s.log.Info().Msg("socket started")

	return nil
}

func (s *ConnectorSocket) nextRequest(ctx context.Context, conn *websocket.Conn) (*http.Request, error) {
	messageType, message, err := conn.Reader(ctx)
	s.redialLock.Unlock() // kick off redial

	if err != nil {
		return nil, fmt.Errorf("RequestReaderError: %w", err)
	}

	s.log.Debug().Msg("request available")

	if messageType != websocket.MessageText {
		s.log.Error().
			Str("expectedMessageType", "textMessage").
			Str("actualMessageType", "binaryMessage").
			Msg("protocol error")

		return nil, errors.New("CrankerProtoError: request not started with text message")
	}

	buf := s.buffers.Get().([]byte)
	defer s.buffers.Put(buf)
	headerSize, err := message.Read(buf)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("RequestReadError: %w", err)
	}

	s.log.Debug().Bytes("recv", buf[0:headerSize]).Msg("wss msg received")

	headers := buf[0 : headerSize-2]
	marker := buf[headerSize-2 : headerSize]
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(headers)))
	if err != nil {
		return nil, err
	}

	req.URL.Path = strings.TrimPrefix(req.URL.Path, s.servicePrefix)

	if bytes.Compare(marker, []byte(markerReqHasNoBody)) == 0 {
		s.log.Debug().Msg("request without body")
	} else if bytes.Compare(marker, []byte(markerReqBodyPending)) == 0 {
		s.log.Debug().Msg("request with body")
		r, w := io.Pipe()
		req.Body = r
		go s.pumpRequestBody(ctx, w, conn)
	} else {
		s.log.Error().Bytes("marker", marker).Msg("unexpected marker")
		return nil, errors.New("UnexpectedMarker")
	}

	return req.WithContext(ctx), nil
}

func (s *ConnectorSocket) pumpRequestBody(ctx context.Context, out *io.PipeWriter, conn *websocket.Conn) {
	buf := s.buffers.Get().([]byte)
	defer s.buffers.Put(buf)
	for {
		s.log.Debug().Msg("draining request body")
		messageType, message, err := conn.Reader(ctx)
		if err != nil {
			s.log.Error().
				AnErr("err", err).
				Msg("failed to create reader for request body")
			return
		}

		switch messageType {
		case websocket.MessageBinary:
			n, err := io.CopyBuffer(out, message, buf)
			if err != nil {
				s.log.Error().
					AnErr("err", err).
					Msg("failed to send request body")
				return
			}

			s.log.Debug().Int64("bytesSent", n).Msg("sending request body")
		case websocket.MessageText:
			n, err := message.Read(buf)

			s.log.Debug().
				Bytes("recv", buf[0:n]).
				Msg("expecting a marker")

			if n == 2 {
				if bytes.Compare([]byte(markerReqBodyEnded), buf[0:2]) == 0 {
					err := out.Close()
					if err != nil {
						s.log.Error().
							Bytes("marker", buf[0:2]).
							Msg("unexpected marker")
						return
					}

					s.log.Debug().
						Msg("request ended")
					return
				}
			}

			s.log.Error().Bytes("recv", buf[0:n]).Msg("protocol error: not a marker")

			if err != nil || err != io.EOF {
				s.log.Error().AnErr("err", err).Msg("error reading marker")
				return
			}
		}
	}
}

func (s *ConnectorSocket) proxyRequest(ctx context.Context, conn *websocket.Conn) {
	defer func(conn *websocket.Conn, code websocket.StatusCode, reason string) {
		err := conn.Close(code, reason)
		if err != nil {
			s.log.Info().Msg("error closing wss connection")
		}
	}(conn, websocket.StatusNormalClosure, "close requested by client")

	s.log.Info().
		Msg("waiting for request")

	req, err := s.nextRequest(ctx, conn)
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

	err = s.sendResponse(ctx, resp, conn)
	if err != nil {
		s.log.Error().AnErr("respErr", err).Msg("error sending response")
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

func (s *ConnectorSocket) sendResponse(ctx context.Context, resp *http.Response, conn *websocket.Conn) error {
	defer resp.Body.Close()

	var headerBuf bytes.Buffer
	_, err := fmt.Fprintf(&headerBuf, "%s %s\r\n", resp.Proto, resp.Status)
	if err != nil {
		return err
	}

	err = resp.Header.Write(&headerBuf)
	if err != nil {
		return err
	}
	s.log.Debug().Bytes("respHeader", headerBuf.Bytes()).Msg("sending response headers")

	// write headers in text
	err = conn.Write(ctx, websocket.MessageText, headerBuf.Bytes())
	if err != nil {
		return err
	}

	buf := s.buffers.Get().([]byte)
	defer s.buffers.Put(buf)

	for {
		nread, err := resp.Body.Read(buf)
		if nread > 0 {
			s.log.Debug().Int("bytesRead", nread).Msg("response read")

			wssWriter, _ := conn.Writer(ctx, websocket.MessageBinary)
			nsent, err := io.Copy(wssWriter, bytes.NewReader(buf[0:nread]))
			if err != nil {
				s.log.Error().AnErr("err", err).Msg("Error sending response")
			}

			wssWriter.Close()

			s.log.Debug().Int64("bytesSent", nsent).Msg("response sent")
		}

		if err != nil && err != io.EOF {
			s.log.Error().AnErr("err", err).Msg("Error reading response from service")
			return err
		}

		if err == io.EOF {
			s.log.Debug().Msg("")
			break
		}
	}

	return nil
}
