FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /linklog cmd/linklog/main.go

FROM node:24-alpine
RUN apk --no-cache add ca-certificates tzdata \
    && npm install -g outline-mcp-server@5.8.5 \
    && npm cache clean --force

WORKDIR /app
COPY --from=builder /linklog /app/linklog

CMD ["/app/linklog"]
