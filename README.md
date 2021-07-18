# go-cranker-connector

a golang port of [Crank4j](https://github.com/danielflower/crank4j)'s [connector](https://github.com/danielflower/crank4j/tree/master/crank4j-connector-embedded)


## install

```bash
go get github.com/JackKCWong/go-cranker-connector
```


## usage

```go
import	(
	"github.com/JackKCWong/go-cranker-connector/pkg/connector/v2"
)
...

conn := connector.Connector{
    ServiceName:       serviceName, // name register to cranker
    ServiceURL:        serviceURL,  // service root URL
    WSSHttpClient:     &http.DefaultHttpClient, // cranker facing http client, used for websocket connection
    ServiceHttpClient: &http.DefaultHttpClient, // service facing http client, used for servicing request/response
    ShutdownTimeout:   5 * time.Second, // grace period for shutdown 
}

// after service ready
conn.Connect(func() []string {
	return []string {"wss://localhost:12345/register"}
}, 2)
```

See `main.go` for usage as a standalone / embedded connector

See [go-cranker-app](https://github.com/JackKCWong/go-cranker-app) embedded usage with [unixsocket](https://en.wikipedia.org/wiki/Unix_domain_socket).

For logging config, see [zerolog](https://github.com/rs/zerolog)


## TODOs

- [x] retry connection with exp backoff.
- [x] streaming body
- [x] graceful shutdown
- [x] enable discovery
- [ ] health monitoring
- [x] ping pong
- [x] documentation
