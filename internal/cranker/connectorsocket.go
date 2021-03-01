package cranker

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-cranker/pkg/config"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const markerReqBodyPending = "_1"
const markerReqHasNoBody = "_2"
const markerReqBodyEnded = "_3"

type ConnectorSocket struct {
	routerURL   string
	serviceName string
	serviceURL  string
	dialer      *websocket.Dialer
	httpClient  *http.Client
	wss         *websocket.Conn
	buf         []byte
}

func NewConnectorSocket(routerURL, serviceName, serviceURL string,
	config *config.RouterConfig, httpClient *http.Client) *ConnectorSocket {
	return &ConnectorSocket{
		routerURL:   routerURL,
		serviceName: serviceName,
		serviceURL:  serviceURL,
		dialer: &websocket.Dialer{
			TLSClientConfig:  config.TLSClientConfig,
			HandshakeTimeout: config.WSHandshakTimeout,
		},
		httpClient: httpClient,
		buf:        make([]byte, 4*1024),
	}
}

func (s *ConnectorSocket) Close() error {
	log.Debug().
		Str("service", s.serviceName).
		Str("router", s.routerURL).
		Msg("closing connector socket")

	if s.wss != nil {
		return s.wss.Close()
	}

	return nil
}

func (s *ConnectorSocket) dial() error {
	if s.dialer == nil {
		return errors.New("dialer is nil. Has the socket been initialized properly?")
	}

	headers := http.Header{}
	headers.Add("CrankerProtocol", "1.0")
	headers.Add("Route", s.serviceName)

	conn, resp, err := s.dialer.Dial(
		fmt.Sprintf("%s/%s", s.routerURL, "register"),
		headers)

	if resp != nil {
		log.Debug().
			Str("status", resp.Status).
			Str("url", s.routerURL).
			Msg("wss connected")
	}

	if err != nil {
		log.Error().
			Str("router", s.routerURL).
			Str("error", err.Error()).
			Msg("failed to connect to cranker router")

		return err
	}

	s.wss = conn

	s.wss.SetPingHandler(func(appData string) error {
		log.Debug().Str("ping:", appData).Send()
		return nil
	})

	s.wss.SetPongHandler(func(appData string) error {
		log.Debug().Str("pong:", appData).Send()
		return nil
	})

	s.wss.SetCloseHandler(func(code int, text string) error {
		log.Info().Int("code", code).
			Str("text", text).
			Str("url", s.routerURL).
			Msg("wss closed")

		return nil
	})

	return nil
}

func (s *ConnectorSocket) Start() error {
	log.Info().
		Str("router", s.routerURL).
		Str("service", s.serviceURL).
		Msg("socket starting")

	err := s.dial()

	if err != nil {
		log.Error().AnErr("err", err).Msg("error dialing")
		return err
	}

	go s.eventloop()

	log.Info().
		Str("router", s.routerURL).
		Str("target", s.serviceURL).
		Msg("socket started")

	return nil
}

func (s *ConnectorSocket) nextRequest() (*http.Request, error) {
	messageType, message, err := s.wss.NextReader()
	if err != nil {
		log.Error().AnErr("err", err).Msg("error reading request headers")
		return nil, err
	}

	if messageType != websocket.TextMessage {
		log.Error().
			Str("expectedMessageType", "textMessage").
			Str("actualMessageType", "binaryMessage").
			Msg("protocal error")

		return nil, err
	}

	n, err := message.Read(s.buf)
	if err != nil && err != io.EOF {
		log.Error().AnErr("err", err).Msg("error reading request headers")
		return nil, err
	}

	log.Debug().Bytes("recv", s.buf[0:n]).Msg("wss msg received")

	reader := bufio.NewReader(bytes.NewReader(s.buf[0:n]))
	firstline, err := reader.ReadString('\n')
	if err != nil {
		log.Error().AnErr("err", err).Msg("error reading 1st line in request")
		return nil, err
	}

	method, url := decomposeMethodAndURL(firstline)
	url = strings.TrimPrefix(url, "/"+s.serviceName)

	var req *http.Request
	if bytes.Compare(s.buf[n-2:n], []byte(markerReqHasNoBody)) == 0 {
		log.Debug().Msg("request has no body")
		req, err = http.NewRequest(method, url, nil)
	} else {
		r, w := io.Pipe()
		req, err = http.NewRequest(method, url, r)
		go s.pumpRequestBody(w)
	}

	if err != nil {
		log.Error().AnErr("err", err).Msg("error creating request")
		return nil, err
	}

	return req, nil
}

func (s *ConnectorSocket) pumpRequestBody(out *io.PipeWriter) error {
	for {
		log.Debug().Msg("draining request body")
		messageType, message, err := s.wss.NextReader()
		if err != nil {
			return err
		}

		switch messageType {
		case websocket.BinaryMessage:
			n, err := io.CopyBuffer(out, message, s.buf)
			if err != nil {
				return err
			}

			log.Debug().Int64("bytesSent", n).Msg("sending request body")
		case websocket.TextMessage:
			n, err := message.Read(s.buf)

			log.Debug().
				Bytes("recv", s.buf[0:n]).
				Msg("expecting a marker")

			if n == 2 {
				if bytes.Compare([]byte(markerReqBodyEnded), s.buf[0:2]) == 0 {
					out.Close()
					return nil
				}
			}

			log.Error().Bytes("recv", s.buf[0:n]).Msg("protocal error: not a marker")
			if err != nil || err != io.EOF {
				log.Error().AnErr("err", err).Msg("error reading marker")
				return err
			}
		}
	}
}

func decomposeMethodAndURL(line string) (string, string) {
	parts := strings.Split(line, " ")
	return parts[0], parts[1]
}

func (s *ConnectorSocket) eventloop() error {
	defer s.Start() // restart socket after servicing request

	log.Info().
		Str("router", s.routerURL).
		Str("target", s.serviceURL).
		Msg("waiting for request")

	req, err := s.nextRequest()
	if err != nil {
		log.Error().AnErr("reqErr", err).Msg("error waiting for request")
		return err
	}

	resp, err := s.sendRequest(req)
	if err != nil {
		log.Error().AnErr("reqErr", err).Msg("error sending request")
		return err
	}

	return s.sendResponse(resp)
}

func (s *ConnectorSocket) sendRequest(req *http.Request) (*http.Response, error) {
	serviceURL, err := url.Parse(s.serviceURL)
	if err != nil {
		log.Error().AnErr("urlErr", err).Send()
		return nil, err
	}

	req.URL = serviceURL.ResolveReference(req.URL)
	req.RequestURI = ""

	log.Debug().
		Str("url", req.URL.String()).
		Msg("prep req url")

	return s.httpClient.Do(req)
}

func (s *ConnectorSocket) sendResponse(resp *http.Response) error {
	defer s.Close()
	defer resp.Body.Close()
	var headerBuf bytes.Buffer
	fmt.Fprintf(&headerBuf, "%s %s\r\n", resp.Proto, resp.Status)
	resp.Header.Write(&headerBuf)
	log.Debug().Bytes("respHeader", headerBuf.Bytes()).Msg("sending response headers")
	err := s.wss.WriteMessage(websocket.TextMessage, headerBuf.Bytes())

	w, err := s.wss.NextWriter(websocket.BinaryMessage)
	if err != nil {
		log.Error().AnErr("writeRespErr", err).Msg("error creating resp writer")
		return err
	}

	defer w.Close()
	n, err := io.CopyBuffer(w, resp.Body, s.buf)

	if err != nil {
		log.Error().AnErr("writeRespErr", err).Send()
		return err
	}

	log.Debug().Int64("bytesSent", n).Msg("response sent")

	return nil
}
