.PHONY: test build vet run update check secrets hooks audit release
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
REVISION ?= $(shell git rev-parse --verify HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -s -w -X github.com/TrueFix/myip/internal/buildinfo.Version=$(VERSION) -X github.com/TrueFix/myip/internal/buildinfo.Revision=$(REVISION) -X github.com/TrueFix/myip/internal/buildinfo.BuildDate=$(BUILD_DATE)
test:
	go test -race ./...
vet:
	go vet ./...
build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/myip ./cmd/myip
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/updater ./cmd/updater
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/bench ./cmd/bench
run:
	go run ./cmd/myip
update:
	go run ./cmd/updater --once
secrets:
	python3 scripts/check-secrets.py
hooks:
	git config core.hooksPath .githooks
audit:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	go mod download github.com/maxmind/geoipupdate/v8@v8.0.0
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -C "$$(go env GOMODCACHE)/github.com/maxmind/geoipupdate/v8@v8.0.0" ./cmd/geoipupdate
check: secrets test vet
	python3 -m unittest discover -s scripts -p 'test_*.py'
	docker compose config --quiet
release:
	BUILD_DATE="$(BUILD_DATE)" sh scripts/release.sh "$(VERSION)" "$(REVISION)"
