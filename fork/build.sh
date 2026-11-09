#!/bin/sh
# Build the fork in Docker. Nothing is installed on the host.
#   fork/build.sh            -> dist/tuios (darwin/arm64)
#   fork/build.sh test PKG.. -> go test in the container (default ./internal/...)
#   fork/build.sh vet        -> go vet ./cmd/... ./internal/...
# Module downloads go through proxy.golang.org; tuios.dev is never contacted
# (GOWORK=off plus the replace block in go.mod, and GONOSUMDB keeps the sumdb
# from being asked about tuios.dev paths).
set -e
cd "$(dirname "$0")/.."
mode="${1:-build}"
[ $# -gt 0 ] && shift
img=golang:1.26
run() {
	docker run --rm \
		-v "$PWD":/src -w /src \
		-v tuios-fork-gomod:/go/pkg/mod \
		-v tuios-fork-gocache:/root/.cache/go-build \
		-e GOWORK=off -e GOTOOLCHAIN=auto \
		-e GONOSUMDB=tuios.dev -e GOPRIVATE=tuios.dev \
		"$@"
}
case "$mode" in
build)
	mkdir -p dist
	ver="$(git describe --tags --always --dirty)-fork"
	run -e CGO_ENABLED=0 -e GOOS=darwin -e GOARCH=arm64 "$img" \
		go build -trimpath -ldflags "-s -w -X main.version=$ver -X main.commit=$(git rev-parse HEAD)" -o dist/tuios ./cmd/tuios
	ls -l dist/tuios
	;;
test)
	run "$img" go test "${@:-./internal/...}"
	;;
vet)
	run "$img" go vet ./cmd/... ./internal/...
	;;
*)
	echo "usage: $0 [build|test [pkgs]|vet]" >&2
	exit 2
	;;
esac
