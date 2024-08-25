# Use the official Golang image as a build stage
FROM golang:1.20 as builder

# Set the working directory
WORKDIR /app

# Copy the go.mod and go.sum files
COPY go.mod go.sum ./

# Download the dependencies
RUN go mod download

# Copy the source code
COPY . .

# Set the target platform for the build
ARG TARGETOS
ARG TARGETARCH

# Build the Go application for the target platform
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 go build -o sanitizetelebot ./

# Use a smaller base image for the final image
FROM alpine:latest

# Set the working directory
WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/sanitizetelebot .

# Command to run the application
CMD ["./sanitizetelebot"]