FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cojira .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -h /home/cojira cojira

COPY --from=build /out/cojira /usr/local/bin/cojira

USER cojira
WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/cojira"]
