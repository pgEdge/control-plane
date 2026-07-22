FROM golang:1.26.5

ENV CGO_ENABLED=0

RUN go install github.com/go-delve/delve/cmd/dlv@latest

COPY ./entrypoint.sh /entrypoint.sh
COPY ./pgedge-control-plane /pgedge-control-plane

# nosemgrep: dockerfile.security.missing-user-entrypoint.missing-user-entrypoint
# Dev-only image; runs as root by design, same reasoning as the production
# Dockerfile (bind-mount ownership management). Trivy's DS-0002 is suppressed
# repo-wide in .trivyignore since inline ignore comments don't work for this
# whole-file check.
ENTRYPOINT [ "/entrypoint.sh" ]
