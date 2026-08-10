FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /payments ./services/payments/main.go

FROM alpine:3.20
COPY --from=builder /payments /payments
EXPOSE 8083
CMD ["/payments"]
