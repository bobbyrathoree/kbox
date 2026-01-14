# Example Dockerfile for payments service
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /payments ./cmd/server

FROM gcr.io/distroless/static-debian12

COPY --from=builder /payments /payments

EXPOSE 3000

ENTRYPOINT ["/payments"]
