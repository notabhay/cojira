FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -h /home/cojira cojira

COPY cojira /usr/local/bin/cojira

USER cojira
WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/cojira"]
