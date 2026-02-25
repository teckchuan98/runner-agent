FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN go build -o /out/runner-agent ./main.go

FROM alpine:3.20
RUN adduser -D -H -u 10001 app
USER app
WORKDIR /app
COPY --from=builder /out/runner-agent /app/runner-agent
EXPOSE 9000
ENTRYPOINT ["/app/runner-agent"]

