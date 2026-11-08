#!/usr/bin/env sh
# Cross-compile swisstransfer for every supported platform into dist/ (POSIX sh, needs Go only).
set -eu
cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
LDFLAGS="-s -w -X github.com/kallioli/swisstransfert-cli/internal/cli.Version=$VERSION"
rm -rf dist && mkdir -p dist
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os="${t%/*}"; arch="${t#*/}"; ext=""
  [ "$os" = "windows" ] && ext=".exe"
  echo "building $os/$arch"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" \
    -o "dist/swisstransfer_${VERSION}_${os}_${arch}${ext}" ./cmd/swisstransfer
done
(cd dist && shasum -a 256 swisstransfer_* > SHA256SUMS 2>/dev/null || sha256sum swisstransfer_* > SHA256SUMS)
echo "done:"; ls -1 dist
