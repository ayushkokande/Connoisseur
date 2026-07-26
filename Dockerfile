FROM golang:1.25-alpine AS build

WORKDIR /src

# Dependencies are copied first so the download layer is reused whenever only
# application source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/connoisseur .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && adduser --disabled-password --no-create-home --uid 10001 connoisseur

WORKDIR /app

COPY --from=build /out/connoisseur /app/connoisseur
# The server reads templates and stylesheets from disk relative to its working
# directory, so both trees have to ship in the image.
COPY templates/ /app/templates/
COPY public/ /app/public/

USER connoisseur

ENV PORT=3000
EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget --quiet --spider "http://127.0.0.1:${PORT}/healthz" || exit 1

ENTRYPOINT ["/app/connoisseur"]
