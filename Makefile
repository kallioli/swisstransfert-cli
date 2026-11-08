VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X github.com/kallioli/swisstransfert-cli/internal/cli.Version=$(VERSION)
TARGETS  = linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: build install test vet dist clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/swisstransfer ./cmd/swisstransfer

install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/swisstransfer

vet:
	gofmt -l . && go vet ./...

test:
	go test ./...

dist: clean
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		out=dist/swisstransfer_$(VERSION)_$${os}_$${arch}; mkdir -p $$out; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$out/swisstransfer$$ext ./cmd/swisstransfer || exit 1; \
		cp README.md LICENSE $$out/; \
		if [ "$$os" = "windows" ]; then (cd dist && zip -qr $$(basename $$out).zip $$(basename $$out)); \
		else tar -C dist -czf $$out.tar.gz $$(basename $$out); fi; \
	done
	@cd dist && shasum -a 256 *.tar.gz *.zip > SHA256SUMS && cat SHA256SUMS

clean:
	rm -rf bin dist
