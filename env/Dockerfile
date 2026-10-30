FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./internal/cmd/...

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/ /usr/local/bin/
COPY env/migrations /usr/local/share/paygo/migrations
ENV PAYGO_MIGRATIONS_DIR=/usr/local/share/paygo/migrations
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/paygo-api"]
