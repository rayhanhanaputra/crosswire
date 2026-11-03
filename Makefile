.PHONY: build test vet selftest check registry lint all serve bridge-sim dist clean

BIN     ?= crosswire
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.Version=$(VERSION)

# Build the single binary for this machine.
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/crosswire

# Every transport end to end against in-process fakes. No network, no hardware.
test:
	go test ./...

vet:
	go vet ./...

# One trial set through every connector, from the built binary.
selftest: build
	./$(BIN) selftest

# Start the workbench on loopback.
serve: build
	./$(BIN) serve

# The device bridge, driving nothing.
bridge-sim: build
	./$(BIN) bridge --simulate --kind led

# Validate the knowledge base against the generated registry.
# Needs: pip install -r tools/requirements.txt
check:
	python3 -I tools/build_registry.py --check

# Regenerate the machine-readable registry from knowledge-base front matter.
registry:
	python3 -I tools/build_registry.py

# Lint Markdown with the same tool and version CI uses.
lint:
	npx -y markdownlint-cli2@0.13.0 "**/*.md" "!LICENSE-CODE" "!LICENSE-DOCS" "!node_modules/**" "!dist/**" "!out/**"

# Everything CI runs.
all: vet test check lint

# Release binaries for the three platforms the README promises.
dist: clean
	mkdir -p dist
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/crosswire-darwin-arm64  ./cmd/crosswire
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/crosswire-darwin-amd64  ./cmd/crosswire
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/crosswire-linux-arm64   ./cmd/crosswire
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/crosswire-linux-amd64   ./cmd/crosswire
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/crosswire-windows-amd64.exe ./cmd/crosswire

clean:
	rm -rf dist $(BIN)
