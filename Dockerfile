FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build CSS (Tailwind standalone, official release — keep version in sync with magefile.go)
RUN apk add --no-cache curl && \
    curl -sL https://github.com/tailwindlabs/tailwindcss/releases/download/v3.4.17/tailwindcss-linux-x64 -o /usr/local/bin/tailwindcss && \
    chmod +x /usr/local/bin/tailwindcss && \
    tailwindcss -i tailwind/input.css -o web/static/css/site.css --minify

# Generate templ (version pinned by go.mod) and sqlc code, then build.
# Migrations are embedded into the binary — the image ships no SQL files.
RUN go install github.com/a-h/templ/cmd/templ@$(go list -m -f '{{.Version}}' github.com/a-h/templ) && \
    go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 && \
    templ generate && \
    sqlc generate
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /bin/server /bin/server
COPY --from=build /src/web /web

ENV ENV=production
EXPOSE 8080
ENTRYPOINT ["/bin/server"]
