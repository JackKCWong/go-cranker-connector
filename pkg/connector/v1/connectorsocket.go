package connector

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const markerReqBodyPending = "_1"
const markerReqHasNoBody = "_2"
const markerReqBodyEnded = "_3"

type connectorSocket struct {
	routerURL   string
	serviceName string
	targetURL   string
	wss         *websocket.Conn
	dialer      *websocket.Dialer
	httpClient  *http.Client
	buf         []byte
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

func (s *connectorSocket) restart() error {
	s.close()

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

func (s *connectorSocket) readRequest() (*http.Request, error) {
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

func (s *connectorSocket) pumpRequestBody(out *io.PipeWriter) error {
	for {
		log.Debug().Msg("draining request body")
		messageType, message, err := s.wss.NextReader()
		if err != nil {
			return err
		}

		switch messageType {
		case websocket.BinaryMessage:
			n, err := io.Copy(out, message)
			if err != nil {
				return err
			}

			log.Debug().Int64("bytesSent", n).Msg("sending request body")
			out.Close()
			return nil
		case websocket.TextMessage:
			n, err := message.Read(s.buf)
			if err != nil || err != io.EOF {
				return err
			}

			log.Debug().
				Bytes("recv", s.buf[0:n]).
				Msg("expecting a marker")

			if n > 2 {
				log.Error().Msg("protocal error: not a marker")
			}

			if bytes.Compare([]byte(markerReqBodyEnded), s.buf[0:2]) == 0 {
				out.Close()
				return nil
			}
		}
	}
}

func decomposeMethodAndURL(line string) (string, string) {
	parts := strings.Split(line, " ")
	return parts[0], parts[1]
}

func (s *connectorSocket) waitForRequest() error {
	defer s.close()

	req, err := s.readRequest()

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

	return s.pumpResponse(resp)
}

func (s *connectorSocket) pumpResponse(resp *http.Response) error {
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
	n, err := io.Copy(w, resp.Body)

	if err != nil {
		log.Error().AnErr("writeRespErr", err).Send()
		return err
	}

	log.Debug().Int64("bytesSent", n).Msg("response sent")

	return nil
}
