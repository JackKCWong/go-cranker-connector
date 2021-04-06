package connector

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/JackKCWong/go-cranker-connector/pkg/config"

	"github.com/JackKCWong/go-cranker-connector/internal/util"
	"github.com/stretchr/testify/assert"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"time"
)

func setupLogger() {
	// log.Logger = log.Output(horizontal.ConsoleWriter{Out: os.Stderr})
	zerolog.TimeFieldFormat = time.RFC3339Nano
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05.000"}).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

func setupTestServer() *httptest.Server {
	tlog := log.With().Str("src", "testServer").Logger()
	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		tlog.Info().
			Msg("received request")

		switch {
		case strings.HasSuffix(r.URL.Path, "/get"):
			fmt.Fprint(w, "world")
		case strings.HasSuffix(r.URL.Path, "/post"):
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(500)
				tlog.Info().Msg("error reading request body\n")
			}

			n, err := w.Write(body)
			if err != nil {
				tlog.Error().AnErr("err", err).Msg("error sending test resp")
				return
			}

			tlog.Info().Int("bytesSent", n).Msg("test resp sent")
		}
	}))

	lsn, _ := net.Listen("tcp", "localhost:9999")
	testServer.Listener.Close()
	testServer.Listener = lsn

	testServer.StartTLS()

	return testServer
}

var (
	testServer    *httptest.Server
	testClient    *http.Client
	tlsSkipVerify *tls.Config
)

func TestMain(t *testing.M) {
	setupLogger()
	testServer = setupTestServer()

	tlsSkipVerify = &tls.Config{InsecureSkipVerify: true}
	testClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsSkipVerify,
			Proxy:           util.OSHttpProxy(),
		},
		Timeout: 1 * time.Second,
	}

	defer testServer.Close()

	t.Run()
}

func newConnector() *Connector {
	return NewConnector(
		&config.RouterConfig{
			TLSClientConfig:   tlsSkipVerify,
			WSHandshakTimeout: 1 * time.Second,
		},
		&config.ServiceConfig{
			HTTPClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: tlsSkipVerify,
					// Proxy:           util.OSHttpProxy(),
				},
			},
		})
}

func TestCanHandleGetRequest(t *testing.T) {
	assert := assert.New(t)
	connector := newConnector()
	err := connector.Connect([]string{"wss://localhost:16489"}, 1, "test1", testServer.URL)
	time.Sleep(200 * time.Millisecond)
	assert.Nilf(err, "failed to connect to cranker")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	defer connector.Shutdown(ctx)

	req, _ := http.NewRequest("GET", "https://localhost:8443/test1/get", nil)
	resp, err := testClient.Do(req)

	assert.Nilf(err, "failed to request to cranker: %q", err)

	defer resp.Body.Close()
	assert.Equal("200 OK", resp.Status)
}

func TestCanHandleReconnect(t *testing.T) {
	assert := assert.New(t)
	connector := newConnector()
	err := connector.Connect([]string{"wss://localhost:16489"}, 2, "test2", testServer.URL)
	time.Sleep(200 * time.Millisecond)
	assert.Nilf(err, "failed to connect to cranker")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	defer connector.Shutdown(ctx)

	for i := 0; i < 6; i++ {
		req, _ := http.NewRequest("GET", "https://localhost:8443/test2/get", nil)
		resp, err := testClient.Do(req)

		assert.Nilf(err, "failed to request to cranker: %q", err)

		defer resp.Body.Close()
		assert.Equal("200 OK", resp.Status)

		time.Sleep(100 * time.Microsecond)
	}
}

func TestCanHandlePostRequest(t *testing.T) {
	assert := assert.New(t)
	connector := newConnector()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	defer connector.Shutdown(ctx)

	err := connector.Connect([]string{"wss://localhost:16489"}, 1, "test3", testServer.URL)
	time.Sleep(200 * time.Millisecond)
	assert.Nilf(err, "failed to connect to cranker")

	req, _ := http.NewRequest("POST",
		"https://localhost:8443/test3/post",
		bytes.NewBufferString("hello world"))

	resp, err := testClient.Do(req)

	assert.Nilf(err, "failed to request to cranker")
	assert.Equal("200 OK", resp.Status)

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	assert.Nilf(err, "failed to read resp from cranker")
	assert.Equal("hello world", string(body))
}
