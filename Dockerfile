# syntax=docker/dockerfile:1
FROM golang:1.26.10-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gopulse ./cmd/server
RUN go install github.com/google/go-licenses/v2@v2.0.1 \
    && CGO_ENABLED=0 go-licenses save ./cmd/server --save_path=/out/third_party_licenses --ignore=github.com/AbdallahAwd/GoPulse

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates openssh-client \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 gopulse \
    && useradd --uid 10001 --gid gopulse --create-home gopulse \
    && mkdir -p /var/lib/gopulse && chown gopulse:gopulse /var/lib/gopulse
COPY --from=build /out/gopulse /usr/local/bin/gopulse
COPY LICENSE /usr/share/doc/gopulse/LICENSE
COPY --from=build /usr/local/go/LICENSE /usr/share/doc/gopulse/GO_LICENSE
COPY --from=build /out/third_party_licenses /usr/share/doc/gopulse/third_party_licenses
USER 10001:10001
WORKDIR /home/gopulse
ENV GOPULSE_SERVER_HOST=0.0.0.0 GOPULSE_DATA_DIR=/var/lib/gopulse
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gopulse"]
