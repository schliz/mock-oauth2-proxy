FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o mock-oauth2-proxy .

FROM scratch
COPY --from=builder /build/mock-oauth2-proxy /mock-oauth2-proxy
EXPOSE 4180
ENTRYPOINT ["/mock-oauth2-proxy"]
