FROM alpine:3.8

RUN apk add --no-cache ca-certificates

ADD ./draughtsman-eventer /draughtsman-eventer

ENTRYPOINT ["/draughtsman-eventer"]
