package main

import (
	"time"

	"github.com/go-cranker/pkg/connector/v1"
	"github.com/rs/zerolog/log"
)	

func main() {
	connector := connector.NewConnector()
	err := connector.Connect([]string{"wss://0.0.0.0:16489"}, 1, "hello", "/hello")

	if err != nil {
		log.Error().AnErr("connectorError", err)
	}

	time.Sleep(5 * time.Minute)
}