# Use an official Golang image as a parent image
FROM golang:latest

# Set the working directory inside the container
WORKDIR /app

# Copy the current directory contents into the container at /app
COPY . .

# Install Kafka Go dependency
RUN go mod download

# Build the test binary
RUN go build -o kafka-test test.go

# Set the environment variable for the Kafka broker
ENV KAFKA_BROKER=kafka:9092

# Run the test binary
CMD ["./kafka-test"]

