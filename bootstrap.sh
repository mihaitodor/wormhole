#!/bin/bash

# This script downloads the entire go toolchain and sets up the build environment
# into the "_build" folder

set -e

GO_VERSION="go1.11.4"

die () {
	echo "${1}"
	exit 1
}

if ! type gcc > /dev/null; then
	die "Please install gcc"
fi

if ! type git > /dev/null; then
	die "Please install git"
fi

mkdir -p _build

if [ ! -d _build/go ]; then
	wget "https://storage.googleapis.com/golang/${GO_VERSION}.linux-amd64.tar.gz" -O "_build/${GO_VERSION}.linux-amd64.tar.gz"

	tar -xvf "_build/${GO_VERSION}.linux-amd64.tar.gz" -C _build
fi
