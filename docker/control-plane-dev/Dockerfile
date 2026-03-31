FROM golang:1.25.8

ENV CGO_ENABLED=0

RUN go install github.com/go-delve/delve/cmd/dlv@latest

COPY ./entrypoint.sh /entrypoint.sh
COPY ./pgedge-control-plane /pgedge-control-plane

# nosemgrep: dockerfile.security.missing-user-entrypoint.missing-user-entrypoint
ENTRYPOINT [ "/entrypoint.sh" ]
