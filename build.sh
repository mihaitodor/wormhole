#!/bin/bash

# This script attempts to build and test this project
# The executable will be written to ./wormhole

set -e

die () {
	echo "${1}"
	exit 1
}

if [ ! -d _build/go ]; then
	die "Please run bootstrap.sh first"
fi

mkdir -p _build/work

export GOROOT="$(pwd)/_build/go"
export GOPATH="$(pwd)/_build/work"
export PATH="${GOROOT}/bin:${PATH}"

go build

go test ./...