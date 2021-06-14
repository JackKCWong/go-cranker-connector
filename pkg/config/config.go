package config

import (
	"crypto/tls"
	"net/http"
	"time"
)

type RouterConfig struct {
	TLSClientConfig   *tls.Config
	WSHandshakTimeout time.Duration
	BufferSize        int // min size 8k
}

type ServiceConfig struct {
	HTTPClient *http.Client
}
