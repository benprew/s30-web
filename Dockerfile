FROM golang:1.26-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /s30-web .
RUN mkdir /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=backend /s30-web /app/s30-web
COPY --from=backend --chown=65532:65532 /data /app/data
COPY public /app/public
ENV PORT=8080
ENV STATIC_DIR=/app/public
ENV DB_PATH=/app/data/s30.db
EXPOSE 8080
ENTRYPOINT ["/app/s30-web"]
