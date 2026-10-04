.PHONY: build test offline-check run clean

GO ?= go
BIN := build/nvr

build:
	mkdir -p build
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/nvr

test:
	$(GO) test ./...

offline-check:
	GOPROXY=off GOSUMDB=off GOFLAGS="-mod=mod" $(GO) test ./...
	./scripts/verify-offline.sh

run:
	$(GO) run ./cmd/nvr

clean:
	rm -rf build
