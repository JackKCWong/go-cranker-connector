package connector

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"time"
)

func setupLogger() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

func setupTestServer() *httptest.Server {
	tlog := log.With().Str("src", "testServer").Logger()
	testServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tlog.Info().Str("url", r.URL.Path).Msg("received request")
		defer r.Body.Close()

		switch r.URL.Path {
		case "/get":
			fmt.Fprint(w, "world")
		case "/post":
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

	return testServer
}

var testServer *httptest.Server

func TestMain(t *testing.M) {
	setupLogger()
	testServer = setupTestServer()
	defer testServer.Close()

	t.Run()
}

func TestCanHandlerGetRequest(t *testing.T) {
	assert := assert.New(t)
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	connector := NewConnectorWithConfig(tlsConfig)
	err := connector.Connect([]string{"wss://0.0.0.0:16489"}, 1, "test1", testServer.URL)
	assert.Nilf(err, "failed to connect to cranker")

	defer connector.Destroy()

	hc := http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		Timeout:   1 * time.Second,
	}

	resp, err := hc.Get("https://localhost:8443/test1/get")

	assert.Nilf(err, "failed to request to cranker")

	defer resp.Body.Close()
	assert.Equal("200 OK", resp.Status)
}

func TestCanHandlerPostRequest(t *testing.T) {
	assert := assert.New(t)
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	connector := NewConnectorWithConfig(tlsConfig)
	err := connector.Connect([]string{"wss://0.0.0.0:16489"}, 1, "test2", testServer.URL)
	assert.Nilf(err, "failed to connect to cranker")

	defer connector.Destroy()

	hc := http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		Timeout:   1 * time.Second,
	}

	resp, err := hc.Post("https://localhost:8443/test2/post",
		"text/plain",
		bytes.NewBufferString("world"))

	assert.Nilf(err, "failed to request to cranker")

	assert.Equal("200 OK", resp.Status)
	defer resp.Body.Close()

	time.Sleep(500 * time.Millisecond)
	body, err := ioutil.ReadAll(resp.Body)
	assert.Nilf(err, "failed to read resp from cranker")
	assert.Equal("world", string(body))
}
