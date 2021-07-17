package main

import (
	"crypto/tls"
	"fmt"
	"github.com/JackKCWong/go-cranker-connector/pkg/config"
	"github.com/JackKCWong/go-cranker-connector/pkg/connector/v1"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	tlsSkipVerify := &tls.Config{InsecureSkipVerify: true}
	conn := connector.NewConnector(
		&config.RouterConfig{
			TLSClientConfig:   tlsSkipVerify,
		},
		&config.ServiceConfig{
			HTTPClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: tlsSkipVerify,
				},
			},
		})

	crankerWss := os.Args[1]
	serviceName := os.Args[2]
	serviceURL := os.Args[3]

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-c
		log.Info().Msg("shutting down...")
		conn.Shutdown()
		log.Info().Msg("shutdown finished")
	}()

	err := conn.Connect([]string{crankerWss}, 2, serviceName, serviceURL)
	if err != nil {
		fmt.Printf("Error connecting cranker %s, err: %q", crankerWss, err)
		return
	}

	wg.Wait()

	os.Exit(0)
}
