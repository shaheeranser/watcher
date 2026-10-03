# Build the static binary, then ship it on a minimal base. The runtime image
# has no shell: the binary runs in the foreground and the platform owns its
# lifecycle (PROD-PKG-5).
FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/watcher ./cmd/watcher

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/watcher /usr/local/bin/watcher

# `watcher run` in a Compose project attaches to the project's siblings by
# default, so no source flags are needed here (PROD-PKG-1).
ENTRYPOINT ["/usr/local/bin/watcher", "run"]
