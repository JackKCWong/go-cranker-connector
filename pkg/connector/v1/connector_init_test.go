package connector

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/JackKCWong/go-cranker-connector/internal/util"
	"github.com/JackKCWong/go-cranker-connector/pkg/config"
	"github.com/mccutchen/go-httpbin/v2/httpbin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"time"
)

func setupLogger() {
	// log.Logger = log.Output(horizontal.ConsoleWriter{Out: os.Stderr})
	zerolog.TimeFieldFormat = time.RFC3339Nano
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05.000"}).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

var (
	testClient      *http.Client
	tlsSkipVerify   *tls.Config
	connector       *Connector
	crankerURL      string
	testServiceName string
)

func init() {
	setupLogger()

	// setup server
	testHandler := httpbin.New()
	testServer := httptest.NewServer(testHandler.Handler())

	// setup client
	tlsSkipVerify = &tls.Config{InsecureSkipVerify: true}
	testClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsSkipVerify,
			Proxy:           util.OSHttpProxy(),
		},
		Timeout: 5 * time.Second,
	}

	// setup connector
	crankerWSS := os.Getenv("CRANKER_TEST_WSS_URL")
	if crankerWSS == "" {
		crankerWSS = "wss://localhost:16489"
	}

	crankerURL = os.Getenv("CRANKER_TEST_URL")
	if crankerURL == "" {
		crankerURL = "https://localhost:8443"
	}

	testServiceName = os.Getenv("CRANKER_TEST_SERVICE")
	if testServiceName == "" {
		testServiceName = "test"
	}

	connector = newConnector()
	err := connector.Connect([]string{crankerWSS}, 2, testServiceName, testServer.URL)
	if err != nil {
		panic(err)
	}

	time.Sleep(200 * time.Millisecond)
}

func testEndpoint(path string) string {
	return fmt.Sprintf("%s/%s%s", crankerURL, testServiceName, path)
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

type HttpBinResp struct {
	Args    Args                       `json:"args"`
	Data    string                     `json:"data"`
	Files   Files                      `json:"files"`
	Form    Form                       `json:"form"`
	Headers Headers                    `json:"headers"`
	JSON    interface{}                `json:"json"`
	Origin  string                     `json:"origin"`
	URL     string                     `json:"url"`
	Cookies map[string]json.RawMessage `json:"cookies"`
}

type Args struct {
}

type Files struct {
}

type Form struct {
}

type Headers struct {
	AcceptEncoding []string `json:"Accept-Encoding"`
	ContentLength  []string `json:"Content-Length"`
	ContentType    []string `json:"Content-Type"`
	Host           []string `json:"Host"`
	UserAgent      []string `json:"User-Agent"`
}

func bin(body io.ReadCloser) (*HttpBinResp, error) {
	binResp := HttpBinResp{}
	buf, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(buf, &binResp)
	if err != nil {
		return nil, err
	}

	return &binResp, nil
}
