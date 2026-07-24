FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gobalancer .

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /gobalancer /usr/local/bin/gobalancer

EXPOSE 8080 9090

ENTRYPOINT ["gobalancer"]
CMD ["-config", "/etc/gobalancer/config.yaml"]
