package connector

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/JackKCWong/go-cranker-connector/internal/util"
	"github.com/mccutchen/go-httpbin/v2/httpbin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func setupLogger() {
	// log.Logger = log.Output(horizontal.ConsoleWriter{Out: os.Stderr})
	zerolog.TimeFieldFormat = time.RFC3339Nano
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05.000"}).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

var (
	testServer      *httptest.Server
	testClient      *http.Client
	tlsSkipVerify   *tls.Config
	connector       *Connector
	crankerURL      string
	testServiceName string
)

func TestMain(t *testing.M) {
	setup()
	t.Run()
	tearDown()
}

func tearDown() {
	connector.Shutdown()
}

func setup() {
	setupLogger()

	// setup server
	testHandler := httpbin.New()
	testServer = httptest.NewServer(testHandler.Handler())

	// setup client
	tlsSkipVerify = &tls.Config{InsecureSkipVerify: true}
	testClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsSkipVerify,
			Proxy:           util.OSHttpProxy(),
		},
		Timeout: 30 * time.Second,
	}

	crankerURL = os.Getenv("CRANKER_TEST_URL")
	if crankerURL == "" {
		crankerURL = "https://localhost:8442"
	}

	testServiceName = os.Getenv("CRANKER_TEST_SERVICE")
	if testServiceName == "" {
		testServiceName = "test"
	}

	connector = &Connector{
		ServiceName: testServiceName,
		ServiceURL:  testServer.URL,
		WSSHttpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsSkipVerify,
			},
		},
		ServiceHttpClient: testClient,
		ShutdownTimeout:   3 * time.Second,
	}

	err := connector.Connect(func() []string {
		crankerWSS := os.Getenv("CRANKER_TEST_WSS_URL")
		if crankerWSS == "" {
			crankerWSS = "wss://localhost:16488/register"
		}

		return []string{crankerWSS}
	}, 2)

	if err != nil {
		panic(err)
	}

	time.Sleep(200 * time.Millisecond)
}

func testEndpoint(path string) string {
	return fmt.Sprintf("%s/%s%s", crankerURL, testServiceName, path)
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

type Expect struct {
	t *testing.T
}

func (e Expect) Nil(v interface{}) {
	require.Nil(e.t, v)
}

func (e Expect) Nilf(v interface{}, msg string, args ...interface{}) {
	require.Nilf(e.t, v, msg, args)
}

func (e Expect) Equal(exp interface{}, actual interface{}) {
	require.Equal(e.t, exp, actual)
}
