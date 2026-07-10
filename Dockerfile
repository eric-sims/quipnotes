# syntax=docker/dockerfile:1

# ---- Build stage ----
FROM golang:1.25-alpine AS build
WORKDIR /app

# Cache module downloads separately from the source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static binary so it runs on a minimal base image.
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/quipnotes .

# ---- Run stage ----
FROM alpine:latest
WORKDIR /app

# Word/prompt data and .env are NOT baked into the image: words.csv is
# proprietary and configuration is environment-specific. They are mounted /
# injected at runtime (see docker-compose.prod.yaml).
COPY --from=build /app/quipnotes .

EXPOSE 8081
CMD ["./quipnotes"]
