FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev git

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o obsidian-goat-sync ./cmd/server/

FROM alpine:3.19
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY --from=builder /build/obsidian-goat-sync .
RUN mkdir -p /app/data

EXPOSE 8080
CMD ["./obsidian-goat-sync"]
