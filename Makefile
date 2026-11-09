GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS ?= -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

.PHONY: all build fmt fmt-check tidy-check vet test test-race test-integration staticcheck gosec vuln licenses check snapshot docker-build run clean

all: check build

build:
	mkdir -p bin
	$(GO) build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o bin/wvc ./cmd/wvc

fmt:
	$(GO) fmt ./...

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" || \
		(echo "Go files need formatting; run 'make fmt'" >&2; gofmt -l $$(find . -name '*.go' -not -path './vendor/*'); exit 1)

tidy-check:
	$(GO) mod tidy
	@if [ -d .git ]; then \
		git diff --exit-code -- go.mod go.sum || \
			(echo "go.mod/go.sum changed; commit the tidy result" >&2; exit 1); \
	else \
		echo "go.mod/go.sum are tidy"; \
	fi

test:
	$(GO) test -count=1 ./...

test-race:
	CGO_ENABLED=1 $(GO) test -race -count=1 ./...

test-integration:
	WVC_TEST_DATABASE_URL="$${WVC_TEST_DATABASE_URL:-postgres://wvc:wvc@127.0.0.1:55433/wvc_test?sslmode=disable}" $(GO) test -count=1 ./...

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...

gosec:
	$(GO) run github.com/securego/gosec/v2/cmd/gosec@v2.30.0 -quiet ./...

vuln:
	PATH="$$($(GO) env GOPATH)/bin:$$PATH" $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

licenses:
	rm -rf third_party
	$(GO) run github.com/google/go-licenses@v1.6.0 save ./... --save_path=third_party --force
	@rm -rf third_party/webvirtcloud-go

check: fmt-check tidy-check vet test

snapshot:
	rm -rf dist
	mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o dist/wvc-linux-amd64 ./cmd/wvc
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o dist/wvc-linux-arm64 ./cmd/wvc
	cp LICENSE NOTICE THIRD_PARTY_LICENSES.md dist/
	cp -a third_party dist/
	cd dist && sha256sum wvc-* > SHA256SUMS

docker-build:
	docker build \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		-t webvirtcloud-go:$(VERSION) .

run: build
	./bin/wvc

clean:
	rm -rf bin dist coverage.out
