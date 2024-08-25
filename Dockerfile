# Use a more recent version of Go
FROM golang:1.22.1 as builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Copy the entire project
COPY . .

# Update go.mod to use the correct Go version
RUN go mod edit -go=1.22

# Download dependencies
RUN go mod download

# Build the Go application for the target platform
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 go build -o sanitizetelebot ./

# Use a smaller base image for the final image
FROM alpine:latest

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/sanitizetelebot .

# Command to run the application
CMD ["./sanitizetelebot"]