package connector

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"time"
)

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

func TestCanConnect(t *testing.T) {
	testServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello world")
	}))

	defer testServer.Close()
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})

	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	connector := NewConnectorWithConfig(tlsConfig)
	err := connector.Connect([]string{"wss://0.0.0.0:16489"}, 1, "hello", testServer.URL)
	if err != nil {
		t.Fatalf("failed to connect to router, %q", err)
	}

	defer connector.Destroy()

	time.Sleep(1 * time.Second)

	log.Debug().Msg("sending test request")
	hc := http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		Timeout:   1 * time.Second,
	}
	resp, err := hc.Get("https://localhost:8443/hello")

	if err != nil {
		t.Fatalf("failed to request via cranker, %q", err)
	}

	respBuf, err := httputil.DumpResponse(resp, true)
	if err != nil {
		t.Fatalf("failed to get response via cranker, %q", err)
	}

	t.Logf("got resp: [%s]", respBuf)

	time.Sleep(1 * time.Second)
}
