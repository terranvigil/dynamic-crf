SHELL := /bin/bash

GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GORUN=$(GOCMD) run
GOTEST=$(GOCMD) test
GOINSTALL=$(GOCMD) install
GOBINPATH=`go env GOPATH`/bin
GOFUMPTPATH=$(GOBINPATH)/gofumpt
GOLANGCI_LINT_VER="1.59.1"
GOFMPT_VER=""

BINARY_NAME=dynamic-crf
APP_IMAGE_NAME=$(BINARY_NAME)-app
FFMPEG_IMAGE_NAME=$(BINARY_NAME)-ffmpeg
BENTO4_IMAGE_NAME=$(BINARY_NAME)-bento4
MEDIAINFO_IMAGE_NAME=$(BINARY_NAME)-mediainfo
X264_IMAGE_NAME=$(BINARY_NAME)-x264
X265_IMAGE_NAME=$(BINARY_NAME)-x265
SVTAV1_IMAGE_NAME=$(BINARY_NAME)-svtav1

build:
	$(GOBUILD) -o $(BINARY_NAME) -v ./cmd/dynamic_crf.go
	chmod +x $(BINARY_NAME)

test:
	$(GOTEST) -v -race -count=1 -parallel=4 -tags=unit ./...

fmt:
	$(GOFUMPTPATH) -w .

lint:
ifneq (${GOLANGCI_LINT_VER}, "$(shell golangci-lint version --format short 2>&1)")
	./install-golangcilint.sh v${GOLANGCI_LINT_VER}
endif
	$(GOBINPATH)/golangci-lint --timeout 3m run --allow-parallel-runners
