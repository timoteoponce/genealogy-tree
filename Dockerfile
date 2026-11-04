# Multi-stage build: Go builder, then a minimal runtime image.
# The binary is fully static (CGO disabled; SQLite is the pure-Go driver)
# and all templates/static assets are embedded, so no extra runtime files.

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/familytree ./cmd/familytree

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/familytree /familytree
VOLUME /data
EXPOSE 8080
# Healthcheck runs the binary itself against /healthz (no curl/wget in distroless).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD ["/familytree", "healthcheck"]
ENTRYPOINT ["/familytree", "serve"]
