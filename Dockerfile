FROM golang:1.26.1-alpine AS builder
WORKDIR /app
RUN apk upgrade --no-cache && apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o ecs-autoscaler

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /app
COPY --from=builder /app/ecs-autoscaler .
ENTRYPOINT ["/app/ecs-autoscaler"]
