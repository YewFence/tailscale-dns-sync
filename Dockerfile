FROM golang:1.23-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o tailscale-dns-sync .

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/tailscale-dns-sync /tailscale-dns-sync
EXPOSE 3000
ENTRYPOINT ["/tailscale-dns-sync"]
