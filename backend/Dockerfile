# Build do control plane (api + worker + bootstrap) num único binário multi-comando.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 go build -o /out/bootstrap ./cmd/bootstrap \
 && CGO_ENABLED=0 go build -o /out/manager-api ./cmd/manager-api

FROM alpine:3.20
# ffmpeg/ffprobe: montagem offline das gravações no worker (docs/manager.md §1).
RUN apk add --no-cache ca-certificates curl ffmpeg
COPY --from=build /out/api /out/worker /out/bootstrap /out/manager-api /usr/local/bin/
# Roda como usuário sem privilégios (não-root): um comprometimento não vira root no container.
RUN adduser -D -H -u 10001 mailin
USER mailin
# 'api' por padrão; override com `command:` para worker/bootstrap.
CMD ["/usr/local/bin/api"]
