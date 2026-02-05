.PHONY: build test-build test test-short test-stress test-install clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GOPM_BIN = test/bin/gopm
TESTAPP_BIN = test/bin/testapp

build:
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=$(VERSION)" -o gopm ./cmd/gopm/

test-build:
	@mkdir -p test/bin
	go build -o $(GOPM_BIN) ./cmd/gopm/
	go build -o $(TESTAPP_BIN) ./test/testapp/
	@echo "Built: $(GOPM_BIN) $(TESTAPP_BIN)"

test: test-build
	go test ./internal/... -v
	go test ./test/integration/ -v -timeout 300s

test-short: test-build
	go test ./test/integration/ -v -short -timeout 120s

test-stress: test-build
	go test ./test/integration/ -v -run TestStress -timeout 600s

test-install: test-build
	sudo go test ./test/integration/ -v -run TestInstall -tags root -timeout 120s

test-race: test-build
	go test -race ./... -timeout 600s

clean:
	rm -rf test/bin/
	rm -f gopm
