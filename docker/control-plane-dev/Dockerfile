FROM golang:1.24.2

ENV CGO_ENABLED=0

RUN go install github.com/go-delve/delve/cmd/dlv@latest

COPY ./entrypoint.sh /entrypoint.sh
COPY ./control-plane /control-plane

ENTRYPOINT [ "/entrypoint.sh" ]
