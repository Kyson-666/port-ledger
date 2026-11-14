GO ?= go
VERSION ?= $(shell grep -oP 'Version = "\K[^"]+' internal/config/version.go 2>/dev/null || echo dev)
LDFLAGS := -s -w
TAGS := netgo,osusergo

.PHONY: all build build-linux build-all test test-verbose vet lint frontend-check image clean run

all: vet test build-all

build:
	CGO_ENABLED=0 $(GO) build -trimpath -tags $(TAGS) -ldflags "$(LDFLAGS)" -o bin/port-ledger .

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -tags $(TAGS) -ldflags "$(LDFLAGS)" -o bin/port-ledger-linux-amd64 .

build-all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -tags $(TAGS) -ldflags "$(LDFLAGS)" -o bin/port-ledger-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -tags $(TAGS) -ldflags "$(LDFLAGS)" -o bin/port-ledger-linux-arm64 .
	@ls -lh bin/ | tail -3

test:
	$(GO) test ./...

test-verbose:
	$(GO) test -v -count=1 ./...

vet:
	$(GO) vet ./...

frontend-check:
	node scripts/check-frontend.mjs

image:
	docker buildx build --platform linux/amd64,linux/arm64 \
	  --build-arg VERSION=$(VERSION) -t port-ledger:$(VERSION) .

run: build
	DATA_DIR=./.localdata ADMIN_PORT=8899 SCAN_ON_START=true ./bin/port-ledger

clean:
	rm -rf bin .localdata
