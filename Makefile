GO ?= go
BIN := bin/logreduce
VERSION := 0.1.0

.PHONY: build test vet fmt check self-test doctor release clean

build:
	$(GO) build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/logreduce

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	@test -z "$$(gofmt -l cmd internal)" || (echo "gofmt needed:"; gofmt -l cmd internal; exit 1)
	# No network in the tool: no net imports anywhere.
	@if grep -R --include='*.go' -E '"net("|/net/)' cmd internal; then echo "network import found"; exit 1; fi

self-test: build
	./$(BIN) self-test

doctor: build
	./$(BIN) doctor

check: fmt vet test self-test doctor

# Static release binaries for linux-amd64 and linux-arm64, plus tarballs
# and a sha256sum file. Binaries are build artifacts, not committed to git.
release: clean
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o dist/logreduce-linux-amd64 ./cmd/logreduce
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w" -o dist/logreduce-linux-arm64 ./cmd/logreduce
	cp plugin.toml README.md LICENSE CHANGELOG.md dist/
	tar -czf dist/logreduce-$(VERSION)-linux-amd64.tar.gz -C dist logreduce-linux-amd64 plugin.toml README.md LICENSE CHANGELOG.md
	tar -czf dist/logreduce-$(VERSION)-linux-arm64.tar.gz -C dist logreduce-linux-arm64 plugin.toml README.md LICENSE CHANGELOG.md
	cd dist && sha256sum logreduce-$(VERSION)-linux-amd64.tar.gz logreduce-$(VERSION)-linux-arm64.tar.gz logreduce-linux-amd64 logreduce-linux-arm64 > sha256sum.txt
	ls -lh dist/

clean:
	rm -rf bin dist
