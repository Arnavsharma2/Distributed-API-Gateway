FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/gateway ./cmd/gateway

FROM alpine:3.20

RUN addgroup -S gatekeeper && adduser -S gatekeeper -G gatekeeper
USER gatekeeper

COPY --from=build /out/gateway /gateway
COPY deploy/docker/gateway.yaml /etc/gatekeeper/gateway.yaml

EXPOSE 8080 9090
ENTRYPOINT ["/gateway"]
