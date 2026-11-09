FROM golang:1.27.3 AS tuios-build

WORKDIR /go/src/app
COPY . .

# VERSION and COMMIT name the build, so the image's `tuios --version` says
# what it is instead of "dev".
ARG VERSION=dev
ARG COMMIT=none
RUN go mod download &&\
  LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtBy=docker" &&\
  CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o /go/bin/tuios ./cmd/tuios

# RUN go vet -v
# RUN go test -v


FROM gcr.io/distroless/static-debian11:nonroot

ENV TERM=xterm-256color
COPY --from=tuios-build /go/bin/tuios /
ENTRYPOINT ["/tuios"]

# There is no EXPOSE: the multiplexer serves no port.
#
# The base stays distroless nonroot. A base with a shell would need an explicit
# USER.
