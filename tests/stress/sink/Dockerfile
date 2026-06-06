# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/sink .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/sink /sink
EXPOSE 9000
ENTRYPOINT ["/sink"]
