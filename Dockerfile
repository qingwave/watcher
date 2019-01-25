FROM alpine
RUN apk add --no-cache curl
ADD watcher /
ENTRYPOINT ["/watcher"]
