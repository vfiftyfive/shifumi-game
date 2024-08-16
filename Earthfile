# Earthfile
VERSION 0.8

# Define the base stage to build the Go binaries
FROM golang:latest
WORKDIR /app
COPY go.mod go.sum .
RUN go mod download
COPY . .

# Target to build the client binary
build-client:
  RUN CGO_ENABLED=0 go build -o client cmd/client/main.go
  SAVE ARTIFACT client

# Target to build the server binary
build-server:
  RUN CGO_ENABLED=0 go build -o server cmd/server/main.go
  SAVE ARTIFACT server

# Target to build all binaries
build-all:
  BUILD +build-client
  BUILD +build-server

# Target to create the final client image
docker-client:
  FROM gcr.io/distroless/static
  WORKDIR /app
  COPY +build-client/client .
  CMD ["/app/client"]
  SAVE IMAGE --push vfiftyfive/shifumi-client

# Target to create the final server image
docker-server:
  FROM gcr.io/distroless/static
  WORKDIR /app
  COPY +build-server/server .
  CMD ["/app/server"]
  SAVE IMAGE --push vfiftyfive/shifumi-server

# Target to build and push all images
docker-all:
  BUILD +docker-client
  BUILD +docker-server

multi:
  BUILD --platform=linux/amd64 --platform=linux/arm64 +docker-all

