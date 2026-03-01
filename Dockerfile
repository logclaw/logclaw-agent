FROM golang:1.22-alpine AS builder
WORKDIR /src
# Copy source first so go mod tidy can resolve all imports
COPY . .
# Generate go.sum — works even without it pre-committed locally
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /agent ./main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /agent /agent
USER nonroot:nonroot
ENTRYPOINT ["/agent"]
