FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/mock-api ./examples/mock-api

FROM alpine:3.20

RUN addgroup -S mockapi && adduser -S mockapi -G mockapi
USER mockapi

COPY --from=build /out/mock-api /mock-api
EXPOSE 3000
ENTRYPOINT ["/mock-api"]
