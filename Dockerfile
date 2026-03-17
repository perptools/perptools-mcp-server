FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/mcp-server ./app/cmd

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

RUN adduser -D -u 1000 appuser
USER appuser

COPY --from=builder /build/mcp-server /usr/local/bin/mcp-server

EXPOSE 8080

ENTRYPOINT ["mcp-server"]
