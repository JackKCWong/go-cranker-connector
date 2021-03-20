# go-cranker-connector

a golang port of [Crank4j](https://github.com/danielflower/crank4j)'s [connector](https://github.com/danielflower/crank4j/tree/master/crank4j-connector-embedded)


## install

```bash
go get github.com/JackKCWong/go-cranker-connector
```


## usage

see `main.go`.

## TODOs

- [x] retry connection with exp backoff. 
- [ ] graceful shutdown: hand waving with cranker.
- [ ] health monitoring
- [ ] dns discovery
- [ ] documentation
