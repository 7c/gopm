.PHONY: build build-all test-build test test-short test-stress test-install test-race clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -s -w -X main.Version=$(VERSION)
GOPM_BIN = test/bin/gopm
TESTAPP_BIN = test/bin/testapp

# --- Build ---

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/gopm ./cmd/gopm/

build-linux-amd64:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/gopm-linux-amd64 ./cmd/gopm/

build-linux-arm64:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/gopm-linux-arm64 ./cmd/gopm/

build-darwin-amd64:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/gopm-darwin-amd64 ./cmd/gopm/

build-darwin-arm64:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/gopm-darwin-arm64 ./cmd/gopm/

build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64
	@echo "Built all platforms in bin/"
	@ls -lh bin/gopm-*

# --- Test ---

test-build:
	@mkdir -p test/bin
	go build -o $(GOPM_BIN) ./cmd/gopm/
	go build -o $(TESTAPP_BIN) ./test/testapp/
	@echo "Built: $(GOPM_BIN) $(TESTAPP_BIN)"

test: test-build
	go test ./internal/... -v
	go test ./test/ -v -timeout 300s

test-short: test-build
	go test ./test/ -v -short -timeout 120s

test-stress: test-build
	go test ./test/ -v -run TestStress -timeout 600s

test-install: test-build
	sudo go test ./test/ -v -run TestInstall -tags root -timeout 120s

test-race: test-build
	go test -race ./... -timeout 600s

# --- Misc ---

clean:
	rm -rf bin/
	rm -rf test/bin/
