package connector

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

type connectorSocket struct {
	routerURL  string
	targetURL  string
	wss        *websocket.Conn
	httpClient *http.Client
}

func (s *connectorSocket) start() error {
	log.Info().
		Str("router", s.routerURL).
		Str("service", s.targetURL).
		Msg("wss socket starting")

	s.wss.SetPingHandler(func(appData string) error {
		log.Debug().Str("ping:", appData).Send()
		return nil
	})

	s.wss.SetPongHandler(func(appData string) error {
		log.Debug().Str("pong:", appData).Send()
		return nil
	})

	s.wss.SetCloseHandler(func(code int, text string) error {
		log.Info().Int("code", code).Str("text", text).Str("uri", s.routerURL).Msg("wss closed")
		return nil
	})

	log.Info().
		Str("router", s.routerURL).
		Str("target", s.targetURL).
		Msg("wss socket started")

	serviceURL, err := url.Parse(s.targetURL)
	if err != nil {
		log.Error().AnErr("urlErr", err).Send()
		return err
	}

	log.Debug().Msg("waiting for message")
	messageType, message, err := s.wss.ReadMessage()
	if err != nil {
		log.Error().AnErr("readMessageErr", err).Send()
		return err
	}

	log.Debug().
		Int("type", messageType).
		Bytes("recv", message).
		Send()

	buf := bytes.NewBuffer(message)
	req, err := http.ReadRequest(bufio.NewReader(buf))

	if err != nil && err != io.EOF {
		log.Error().AnErr("readRequestErr", err).Send()
		return err
	}

	req.URL = serviceURL.ResolveReference(req.URL)
	req.RequestURI = ""

	log.Debug().Str("url", req.URL.String()).Msg("prep req url")
	resp, err := s.httpClient.Do(req)

	if err != nil {
		log.Error().AnErr("doRequestErr", err).Send()
		return err
	}

	respDump, err := httputil.DumpResponse(resp, true)

	log.Debug().Bytes("resp", respDump).Msg("sending response")
	err = s.wss.WriteMessage(websocket.BinaryMessage, respDump)

	if err != nil {
		log.Error().AnErr("writeRespErr", err).Send()
		return err
	}

	s.wss.Close()

	return nil
}

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
