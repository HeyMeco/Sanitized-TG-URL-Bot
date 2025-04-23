# Use Alpine as the base image
FROM alpine:latest

# Accept build argument for the binary path
ARG APP_BINARY

WORKDIR /root/

# Copy the pre-built binary from bin directory
COPY bin/${APP_BINARY} ./sanitizetelebot

# Make the binary executable
RUN chmod +x ./sanitizetelebot

# Command to run the application
CMD ["./sanitizetelebot"]