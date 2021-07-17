package config

import (
	"crypto/tls"
	"net/http"
)

type RouterConfig struct {
	TLSClientConfig   *tls.Config
	BufferSize        int // min size 8k
}

type ServiceConfig struct {
	HTTPClient *http.Client
}
