package util

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"

	"github.com/rs/zerolog/log"
)

func OSHttpProxy() func(*http.Request) (*url.URL, error) {
	var (
		proxy    string
		proxyURL *url.URL
		err      error
	)

	proxy = os.Getenv("HTTP_PROXY")
	if proxy != "" {
		proxyURL, err = url.Parse(proxy)
		if err != nil {
			panic("HTTP_PROXY is not a valid url.")
		}
	}

	if proxyURL != nil {
		log.Debug().
			Str("proxyURL", proxyURL.String()).
			Msg("using HTTP_RPOXY")

		return http.ProxyURL(proxyURL)
	}

	return nil
}

func OSHttpsProxy() func(*http.Request) (*url.URL, error) {
	var (
		proxy    string
		proxyURL *url.URL
		err      error
	)

	proxy = os.Getenv("HTTPS_PROXY")
	if proxy != "" {
		proxyURL, err = url.Parse(proxy)
		if err != nil {
			panic("HTTPS_PROXY is not a valid url.")
		}
	}

	if proxyURL != nil {
		log.Debug().
			Str("proxyURL", proxyURL.String()).
			Msg("using HTTPS_PROXY")

		return http.ProxyURL(proxyURL)
	}

	return nil
}

func Trace(req *http.Request) *http.Request {
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() {
			fmt.Printf("TLS handshake start\n")
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			fmt.Printf("TLS handshake done: %+v, %q\n", state, err)
		},
		ConnectStart: func(network, addr string) {
			fmt.Printf("Connect start: %s, %s\n", network, addr)
		},
		ConnectDone: func(network, addr string, err error) {
			fmt.Printf("Connect done: %q\n", err)
		},
		GotConn: func(connInfo httptrace.GotConnInfo) {
			fmt.Printf("Got Conn: %+v\n", connInfo)
		},
		DNSDone: func(dnsInfo httptrace.DNSDoneInfo) {
			fmt.Printf("DNS Info: %+v\n", dnsInfo)
		},
		WroteRequest: func(wri httptrace.WroteRequestInfo) {
			fmt.Printf("WroteRequest: %+v\n", wri)
		},
		GetConn: func(hostPort string) {
			fmt.Printf("GetConn: %s\n", hostPort)
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	return req
}
