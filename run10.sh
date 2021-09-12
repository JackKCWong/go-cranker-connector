#!/usr/bin/env bash

arg1=$1
rounds=${arg1:=10}

echo running $rounds rounds

for i in $(seq 1 $rounds); do
  go test -count=1 -race ./...
  if [[ $? -ne 0 ]]; then
    echo test failed
    exit 1
  fi
done
