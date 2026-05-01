FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /linklog cmd/linklog/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata nodejs npm

WORKDIR /app
COPY --from=builder /linklog /app/linklog

CMD ["/app/linklog"]
