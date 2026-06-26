# Сгенерированные templ/CSS-артефакты уже находятся в репозитории.
FROM golang:1.25 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM alpine:3.22

# Исходящие HTTPS-запросы к ядру и Google требуют корневых CA-сертификатов.
RUN apk add --no-cache ca-certificates
COPY --from=build /out/server /server

EXPOSE 8080
ENTRYPOINT ["/server"]
