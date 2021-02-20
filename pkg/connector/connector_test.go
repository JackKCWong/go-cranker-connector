package connector

import (
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

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

func TestCanConnect(t *testing.T) {
	assert := assert.New(t)
	testServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello world")
	}))

	defer testServer.Close()
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})

	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	connector := NewConnectorWithConfig(tlsConfig)
	err := connector.Connect([]string{"wss://0.0.0.0:16489"}, 1, "hello", testServer.URL)
	assert.Nilf(err, "failed to connect to cranker")

	defer connector.Destroy()

	time.Sleep(1 * time.Second)

	log.Debug().Msg("sending test request")
	hc := http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		Timeout:   1 * time.Second,
	}
	resp, err := hc.Get("https://localhost:8443/hello")

	assert.Nilf(err, "failed to request to cranker")

	assert.Equal("200 OK", resp.Status)
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	assert.Nilf(err, "failed to read resp from cranker")
	assert.Equal("hello world", string(body))
}
