BINARY  := ampworth
CMD     := ./cmd/ampworth
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# Point the CLI at another copy of the API, for example a local server:
#   make run ARGS="list" AMPWORTH_API=http://localhost:8080/api/v1
AMPWORTH_API ?=

# Bare `make` prints the target list.
.DEFAULT_GOAL := help

.PHONY: build install run test cover fmt vet check snapshot release clean help

## build: the binary, in this folder
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) $(CMD)

## install: the binary, into your Go bin directory
install:
	CGO_ENABLED=0 go install -trimpath -ldflags="$(LDFLAGS)" $(CMD)

## run: build and run with ARGS, for example make run ARGS="search flow temperature"
run: build
	AMPWORTH_API=$(AMPWORTH_API) ./$(BINARY) $(ARGS)

## test: run the tests
test:
	go test ./...

## cover: the tests, with coverage
cover:
	go test -cover ./...

## fmt: format the code
fmt:
	gofmt -w .

## vet: run go vet
vet:
	go vet ./...

## check: what CI runs: formatting, vet and the tests
check:
	@test -z "$$(gofmt -l .)" || { echo "These files need gofmt:"; gofmt -l .; exit 1; }
	go vet ./...
	go test ./...

## snapshot: build every release binary locally, into dist/, without publishing
snapshot:
	goreleaser release --snapshot --clean

## release: tag VERSION and push it, which makes GitHub build the release
#
#   make release VERSION=v0.1.0
release: check
	@case "$(VERSION)" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "Set VERSION, for example make release VERSION=v0.1.0"; exit 1;; esac
	@test -z "$$(git status --porcelain)" || { echo "Commit or stash your changes first."; exit 1; }
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)

## clean: remove build output
clean:
	rm -rf $(BINARY) dist

## help: list the targets
help:
	@echo "Targets:"
	@awk '/^## [a-z]+:/ { sub(/^## /, ""); i = index($$0, ":"); printf "  %-10s %s\n", substr($$0, 1, i - 1), substr($$0, i + 2) }' $(MAKEFILE_LIST)
