# go-cranker-connector

a golang port of [Crank4j](https://github.com/danielflower/crank4j)'s [connector](https://github.com/danielflower/crank4j/tree/master/crank4j-connector-embedded)


## install

```bash
go get github.com/JackKCWong/go-cranker-connector
```


## usage

see `main.go` for usage as a standalone / embedded connector

see [go-cranker-app](https://github.com/JackKCWong/go-cranker-app) embedded usage with [unixsocket](https://en.wikipedia.org/wiki/Unix_domain_socket).


## TODOs

- [x] retry connection with exp backoff.
- [x] streaming body
- [ ] sse
- [ ] graceful shutdown: hand waving with cranker.
- [ ] health monitoring
- [ ] dns discovery
- [ ] documentation
