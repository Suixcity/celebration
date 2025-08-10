# Build stage
FROM golang:1.24 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./Server.go

# Runtime (distroless)
FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /app/server /app/server
# Optional: if your server reads /etc/secrets/admin_key.txt (per our code)
# We'll mount the secret file there at runtime via Cloud Run.
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/server"]
