package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/JackKCWong/go-cranker-connector/internal/util"
	"github.com/JackKCWong/go-cranker-connector/internal/util/pools"
	"github.com/JackKCWong/go-cranker-connector/internal/util/retry"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"nhooyr.io/websocket"
	"strings"
	"time"
)

var buffers *pools.BufferPool = pools.NewBufferPool()

type WssWorker struct {
	ServiceName     string
	RegisterURL     string
	ServiceURL      string
	ShutdownTimeout time.Duration
	log             zerolog.Logger
	conn            *websocket.Conn
	servicePrefix   string
	id              string
}

func (w *WssWorker) init() error {
	w.id = uuid.NewString()
	w.log = log.With().
		Str("connId", w.id).
		Str("routerURL", w.RegisterURL).
		Str("serviceURL", w.ServiceURL).
		Str("serviceName", w.ServiceName).
		Logger()

	w.servicePrefix = "/" + w.ServiceName

	return nil
}

func (w *WssWorker) Dial(sigTerm context.Context, hc *http.Client) error {
	err := w.init()
	w.log.Info().Msg("dialing")
	if err != nil {
		w.log.Err(err).Msg("failed to init WssWorker")
		return err
	}

	headers := http.Header{}
	headers.Add("CrankerProtocol", "1.0")
	headers.Add("Route", w.ServiceName)

	backoff := retry.Randomize(&retry.ExpBackoff{MinInterval: 5 * time.Second, MaxInterval: 30 * time.Second}, 5*time.Second)

	conn, err := retry.Retry(func() (interface{}, error) {
		dialCtx, cancelDial := context.WithTimeout(sigTerm, 30*time.Second)
		defer cancelDial()

		conn, resp, err := websocket.Dial(
			dialCtx,
			w.RegisterURL,
			&websocket.DialOptions{
				HTTPClient: hc,
				HTTPHeader: headers,
			})

		if err != nil {
			w.log.Error().
				Err(err).
				Msg("failed to connect to cranker router")

			if errors.Is(err, context.Canceled) {
				// stop retry
				return nil, retry.EndOfRetry
			} else {
				// timeout during dial, retry
				return nil, err
			}
		} else {
			w.log.Info().
				Str("status", resp.Status).
				Msg("wss connected")

			return conn, nil
		}
	}, retry.AsBackoff(func(err error) (time.Duration, error) {
		duration, err := backoff.Backoff(err)
		if err == nil {
			w.log.Info().Int64("afterMs", duration.Milliseconds()).Msg("backoff")
		}

		return duration, err
	}))

	if err != nil {
		return err
	}

	w.conn = conn.(*websocket.Conn)

	return nil
}

func (w *WssWorker) nextRequest(sigTerm context.Context, buf []byte) (*http.Request, error) {
	messageType, message, err := w.conn.Reader(sigTerm)

	if err != nil {
		return nil, fmt.Errorf("RequestReaderError: %w", err)
	}

	w.log.Debug().Msg("request available")

	if messageType != websocket.MessageText {
		w.log.Error().
			Str("expectedMessageType", "textMessage").
			Str("actualMessageType", "binaryMessage").
			Msg("protocol error")

		return nil, errors.New("CrankerProtoError: request not started with text message")
	}

	headers := buffers.Get()
	defer buffers.Release(headers)
	headerSize, err := io.CopyBuffer(headers, message, buf)

	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("RequestReadError: %w", err)
	}

	w.log.Debug().Int64("bytesRecv", headerSize).Msg("received headers")

	marker := headers.Bytes()[headerSize-2:]
	headers.Truncate(int(headerSize - 2))
	req, err := http.ReadRequest(bufio.NewReader(headers))
	if err != nil {
		return nil, err
	}

	w.log.Info().
		Str("url", req.URL.String()).
		Msg("received request")

	req.URL.Path = strings.TrimPrefix(req.URL.Path, w.servicePrefix)

	sigKill := util.WithGrace(sigTerm, w.ShutdownTimeout)

	if bytes.Compare(marker, []byte(MarkerReqHasNoBody)) == 0 {
		w.log.Debug().Msg("request without body")
	} else if bytes.Compare(marker, []byte(MarkerReqBodyPending)) == 0 {
		w.log.Debug().Msg("request with body")
		in, out := io.Pipe()
		req.Body = in
		go w.pumpRequestBody(sigKill, out, buf)
	} else {
		w.log.Error().Bytes("marker", marker).Msg("unexpected marker")
		return nil, errors.New("UnexpectedMarker")
	}

	return req.WithContext(sigKill), nil
}

func (w *WssWorker) pumpRequestBody(ctx context.Context, out *io.PipeWriter, buf []byte) {
	for {
		w.log.Debug().Msg("draining request body")
		messageType, message, err := w.conn.Reader(ctx)
		if err != nil {
			w.log.Error().
				AnErr("err", err).
				Msg("failed to create reader for request body")
			return
		}

		switch messageType {
		case websocket.MessageBinary:
			n, err := io.CopyBuffer(out, message, buf)
			if err != nil {
				w.log.Error().
					AnErr("err", err).
					Msg("failed to send request body")
				return
			}

			w.log.Debug().Int64("bytesSent", n).Msg("sending request body")
		case websocket.MessageText:
			n, err := message.Read(buf)

			w.log.Debug().
				Bytes("recv", buf[0:n]).
				Msg("expecting a marker")

			if n == 2 {
				if bytes.Compare([]byte(MarkerReqBodyEnded), buf[0:2]) == 0 {
					err := out.Close()
					if err != nil {
						w.log.Error().
							Bytes("marker", buf[0:2]).
							Msg("unexpected marker")
						return
					}

					w.log.Debug().
						Msg("request ended")
					return
				}
			}

			w.log.Error().Bytes("recv", buf[0:n]).Msg("protocol error: not a marker")

			if err != nil || err != io.EOF {
				w.log.Error().AnErr("err", err).Msg("error reading marker")
				return
			}
		}
	}
}

func (w *WssWorker) Serve(sigTerm context.Context, sem *semaphore.Weighted, client *http.Client, buf []byte) error {
	defer func(conn *websocket.Conn, code websocket.StatusCode, reason string) {
		err := conn.Close(code, reason)
		if err != nil {
			w.log.Info().Msg("error closing wss connection")
		}
	}(w.conn, websocket.StatusNormalClosure, "close requested by client")

	w.log.Info().
		Msg("waiting for request")

	req, err := w.nextRequest(sigTerm, buf)
	sem.Release(1)
	if err != nil {
		w.log.Error().AnErr("readReqErr", err).Msg("error reading request")
		return err
	}

	sigKill := util.WithGrace(sigTerm, w.ShutdownTimeout)
	req = req.WithContext(sigKill)

	resp, err := w.sendRequest(client, req)
	if err != nil {
		errId := uuid.NewString()
		w.log.Error().
			AnErr("reqErr", err).
			Str("errorId", errId).
			Msg("error sending request")

		resp = &http.Response{
			Status:     "500 Server Error",
			StatusCode: 500,
			Body:       ioutil.NopCloser(bytes.NewBufferString(fmt.Sprintf("errorId=%s\n", errId))),
		}
	}

	err = w.sendResponse(sigKill, resp, buf)
	if err != nil {
		w.log.Error().AnErr("respErr", err).Msg("error sending response")
		return err
	}

	return nil
}

func (w *WssWorker) sendRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	serviceURL, err := url.Parse(w.ServiceURL)
	if err != nil {
		return nil, fmt.Errorf("InvalidServiceURLError: %w", err)
	}

	req.URL = serviceURL.ResolveReference(req.URL)
	req.RequestURI = ""

	w.log.Info().
		Str("url", req.URL.String()).
		Msg("proxying request")

	return client.Do(req)
}

func (w *WssWorker) sendResponse(sigKill context.Context, resp *http.Response, buf []byte) error {
	defer resp.Body.Close()

	var headerBuf *bytes.Buffer = buffers.Get()
	defer buffers.Release(headerBuf)

	_, err := fmt.Fprintf(headerBuf, "%s %s\r\n", resp.Proto, resp.Status)
	if err != nil {
		return err
	}

	err = resp.Header.Write(headerBuf)
	if err != nil {
		return err
	}

	w.log.Debug().Bytes("respHeader", headerBuf.Bytes()).Msg("sending response headers")

	// write headers in text
	err = w.conn.Write(sigKill, websocket.MessageText, headerBuf.Bytes())
	if err != nil {
		return err
	}

	for {
		nread, err := resp.Body.Read(buf)
		if nread > 0 {
			w.log.Debug().Int("bytesRead", nread).Msg("response read")

			wssWriter, _ := w.conn.Writer(sigKill, websocket.MessageBinary)
			nsent, err := io.Copy(wssWriter, bytes.NewReader(buf[0:nread]))
			if err != nil {
				w.log.Error().AnErr("err", err).Msg("Error sending response")
			}

			err = wssWriter.Close()
			if err != nil {
				return err
			}

			w.log.Debug().Int64("bytesSent", nsent).Msg("response sent")
		}

		if err != nil && err != io.EOF {
			w.log.Error().AnErr("err", err).Msg("Error reading response from service")
			return err
		}

		if err == io.EOF {
			break
		}
	}

	return nil
}
