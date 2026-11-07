GO ?= go
BIN := bin/logreduce
VERSION := 0.1.0

.PHONY: build test vet fmt check self-test doctor release clean validate cover race fuzz

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

# Validate plugin.toml and the JSON schemas parse and carry the
# contract values. Needs python3 (present on Ubuntu CI runners).
validate:
	python3 scripts/validate.py

self-test: build
	./$(BIN) self-test

doctor: build
	./$(BIN) doctor

check: fmt vet validate test self-test doctor

# Coverage report for the whole module (cmd/ is covered by the CLI
# end-to-end tests in cmd/logreduce).
cover:
	$(GO) test -count=1 -coverprofile=cover.out ./...
	$(GO) tool cover -func=cover.out

# Race detector run. Slower (race-instrumented build); not part of
# `check` so a normal edit/test loop stays fast.
race:
	$(GO) test -count=1 -race ./...

# Fuzz every parser for 30s each. Parsers must never panic on
# arbitrary bytes; a failure writes the crashing input to
# internal/reduce/testdata/fuzz/<name>/ for reproduction.
fuzz:
	for f in FuzzParseGeneric FuzzParseTSC FuzzParseVitest FuzzParsePlaywright FuzzParseESLint FuzzParseTRX FuzzParsePytest FuzzParseGoTest; do \
	  $(GO) test -count=1 -run='^$$' -fuzz=$$f -fuzztime=30s ./internal/reduce/ || exit 1; \
	done

# Static release binaries for linux-amd64 and linux-arm64, plus tarballs
# and a sha256sum file. Binaries are build artifacts, not committed to git.
#
# The build is reproducible: -trimpath and -buildvcs=false remove local
# paths and VCS stamps, -buildid= removes the per-build ID, and the
# tarballs are created with fixed owner, mtime and sort order and
# gzip -n (no filename/timestamp in the gzip header). Two `make release`
# runs from the same source produce byte-identical files; verify with:
#   make release && cp dist/sha256sum.txt /tmp/a.txt && make release && diff /tmp/a.txt dist/sha256sum.txt
release: clean
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -buildvcs=false -ldflags "-s -w -buildid=" -o dist/logreduce-linux-amd64 ./cmd/logreduce
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -buildvcs=false -ldflags "-s -w -buildid=" -o dist/logreduce-linux-arm64 ./cmd/logreduce
	cp plugin.toml README.md LICENSE CHANGELOG.md dist/
	tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner -I 'gzip -n' -cf dist/logreduce-$(VERSION)-linux-amd64.tar.gz -C dist logreduce-linux-amd64 plugin.toml README.md LICENSE CHANGELOG.md
	tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner -I 'gzip -n' -cf dist/logreduce-$(VERSION)-linux-arm64.tar.gz -C dist logreduce-linux-arm64 plugin.toml README.md LICENSE CHANGELOG.md
	cd dist && sha256sum logreduce-$(VERSION)-linux-amd64.tar.gz logreduce-$(VERSION)-linux-arm64.tar.gz logreduce-linux-amd64 logreduce-linux-arm64 > sha256sum.txt
	ls -lh dist/

clean:
	rm -rf bin dist cover.out
