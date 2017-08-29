FROM alpine:3.4

RUN apk add --no-cache ca-certificates

ADD ./draughtsman-eventer /draughtsman-eventer

ENTRYPOINT ["/draughtsman-eventer"]
