FROM gcr.io/distroless/static-debian12

ARG TARGETOS
ARG TARGETARCH
ARG ARCHIVE_VERSION

# nosemgrep: dockerfile.security.missing-user-entrypoint.missing-user-entrypoint
ENTRYPOINT ["/pgedge-control-plane"]

ADD pgedge-control-plane_${ARCHIVE_VERSION}_${TARGETOS}_${TARGETARCH}.tar.gz /
