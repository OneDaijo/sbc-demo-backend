#!/usr/bin/env bash

# Installing required deps
go get cloud.google.com/go/datastore
go get firebase.google.com/go
go get github.com/gorilla/mux
go get

# Autolint in place
go fmt

# Building binary
go build || exit $

get_abs_filename() {
  # $1 : relative filename
  echo "$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
}

export GOOGLE_APPLICATION_CREDENTIALS=$(get_abs_filename "./cloud_credentials.json")

./server
