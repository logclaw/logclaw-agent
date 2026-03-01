FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /agent ./main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /agent /agent
USER nonroot:nonroot
ENTRYPOINT ["/agent"]
