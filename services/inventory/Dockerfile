FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /inventory ./services/inventory/main.go

FROM alpine:3.20
COPY --from=builder /inventory /inventory
EXPOSE 8082
CMD ["/inventory"]
