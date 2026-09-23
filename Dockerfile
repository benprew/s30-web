FROM golang:1.24-bookworm AS backend
WORKDIR /src
COPY go.mod ./
COPY main.go server.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /s30-web .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=backend /s30-web /app/s30-web
COPY public /app/public
ENV PORT=8080
ENV STATIC_DIR=/app/public
EXPOSE 8080
ENTRYPOINT ["/app/s30-web"]
