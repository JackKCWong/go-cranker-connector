package config

import (
	"crypto/tls"
	"net/http"
	"time"
)

type RouterConfig struct {
	TLSClientConfig   *tls.Config
	WSHandshakTimeout time.Duration
}

type ServiceConfig struct {
	HTTPClient *http.Client
}
