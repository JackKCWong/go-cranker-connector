package connector

import (
	"bufio"
	"bytes"
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