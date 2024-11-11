# Build stage
FROM golang:1.23.2-alpine AS build
WORKDIR /app
COPY . .
RUN go build -o /app/quipnotes

# Run stage
FROM alpine:latest
WORKDIR /app
# Copy the compiled binary from the build stage
COPY --from=build /app/quipnotes .

# Copy the .env file to the run stage
COPY --from=build /app/.env .env
COPY --from=build /app/data/words.csv words.csv
COPY --from=build /app/static/index.html static/index.html

EXPOSE 8080
CMD ["./quipnotes"]
