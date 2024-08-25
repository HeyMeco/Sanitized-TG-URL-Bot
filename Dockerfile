# Use the official Golang image as a build stage
FROM --platform=$BUILDPLATFORM golang:1.22.1 AS builder

# Set the working directory
WORKDIR /app

# Copy the go.mod and go.sum files
COPY go.mod go.sum ./

# Download the dependencies
RUN go mod download

# Copy the source code
COPY . .

# Set the target platform for the build
ARG TARGETPLATFORM

# Build the Go application for the target platform
RUN case "${TARGETPLATFORM}" in \
    "linux/amd64") GOARCH=amd64 ;; \
    "linux/arm64") GOARCH=arm64 ;; \
    *) echo "Unsupported platform: ${TARGETPLATFORM}" && exit 1 ;; \
    esac && \
    GOOS=linux CGO_ENABLED=0 go build -o sanitizetelebot ./

# Use a smaller base image for the final image
FROM --platform=$TARGETPLATFORM alpine:latest

# Set the working directory
WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/sanitizetelebot .

# Command to run the application
CMD ["./sanitizetelebot"]