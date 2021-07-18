package main

import (
	"crypto/tls"
	"fmt"
	"github.com/JackKCWong/go-cranker-connector/pkg/connector/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	tlsSkipVerify := &tls.Config{InsecureSkipVerify: true}

	crankerWss := os.Args[1]
	serviceName := os.Args[2]
	serviceURL := os.Args[3]

	conn := connector.Connector{
		ServiceName:         serviceName,
		ServiceURL:          serviceURL,
		WSSHttpClient:       &http.Client{Transport: &http.Transport{TLSClientConfig: tlsSkipVerify}},
		ServiceHttpClient:   &http.Client{Transport: &http.Transport{TLSClientConfig: tlsSkipVerify}},
		ShutdownTimeout:     5 * time.Second,
		RediscoveryInterval: 5 * time.Second,
	}

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

	crankers := strings.Split(crankerWss, ",")
	urls := make([]string, len(crankers))
	for i, wss := range crankers {
		urls[i] = fmt.Sprintf("%s/%s", wss, "register")
	}

	idx := 0
	err := conn.Connect(func() []string {
		// for demo purpose, it swings between crankers.
		idx++
		return []string{urls[idx%len(urls)]}
	}, 2)

	if err != nil {
		fmt.Printf("Error connecting cranker %s, err: %q", crankerWss, err)
		return
	}

	wg.Wait()

	os.Exit(0)
}
