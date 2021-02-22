package connector

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const marker_req_has_body = "_1"
const marker_req_has_no_body = "_2"
const marker_req_body_ended = "_3"

type connectorSocket struct {
	routerURL   string
	serviceName string
	targetURL   string
	wss         *websocket.Conn
	dialer      *websocket.Dialer
	httpClient  *http.Client
}

func (s *connectorSocket) close() error {
	if s.wss != nil {
		return s.wss.Close()
	}

	return nil
}

func (s *connectorSocket) dial() error {
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

func (s *connectorSocket) start() error {
	log.Info().
		Str("router", s.routerURL).
		Str("service", s.targetURL).
		Msg("socket starting")

	err := s.dial()

	if err != nil {
		log.Error().AnErr("dialErr", err).Send()
		return err
	}

	go s.waitForRequest()

	log.Info().
		Str("router", s.routerURL).
		Str("target", s.targetURL).
		Msg("socket started")

	return nil
}

func (s *connectorSocket) waitForRequest() error {
	defer s.close()

	messageType, message, err := s.wss.ReadMessage()
	if err != nil {
		log.Error().AnErr("readMessageErr", err).Send()
		return err
	}

	log.Debug().
		Int("type", messageType).
		Bytes("payload", message).
		Msg("message received")

	if messageType != websocket.TextMessage {
		err := errors.New("INVALID_PROTOCOL")
		log.Error().
			AnErr("err", err).
			Msg("expecting a text message as request headers")

		return err
	}

	var buf bytes.Buffer
	if strings.HasSuffix(string(message), marker_req_has_no_body) {
		buf.Write(message[0 : len(message)-2])
	}

	req, err := http.ReadRequest(bufio.NewReader(&buf))

	if err != nil && err != io.EOF {
		log.Error().AnErr("readRequestErr", err).Send()
		return err
	}

	serviceURL, err := url.Parse(s.targetURL)
	if err != nil {
		log.Error().AnErr("urlErr", err).Send()
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

	defer resp.Body.Close()

	var headerBuf bytes.Buffer
	fmt.Fprintf(&headerBuf, "%s %s\n", resp.Proto, resp.Status)
	resp.Header.Write(&headerBuf)
	log.Debug().Bytes("respHeader", headerBuf.Bytes()).Msg("sending response headers")
	err = s.wss.WriteMessage(websocket.TextMessage, headerBuf.Bytes())

	respDump, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error().AnErr("doResponseErr", err).Send()
		return err
	}

	log.Debug().Bytes("resp", respDump).Msg("sending response")
	err = s.wss.WriteMessage(websocket.BinaryMessage, respDump)

	if err != nil {
		log.Error().AnErr("writeRespErr", err).Send()
		return err
	}

	return nil
}
