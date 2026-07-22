FROM gcr.io/distroless/static-debian12:latest@sha256:61b7ccecebc7c474a531717de80a94709d20547cdcdaf740c25876f2a8e38b44

ARG TARGETOS
ARG TARGETARCH
ARG ARCHIVE_VERSION

# nosemgrep: dockerfile.security.missing-user-entrypoint.missing-user-entrypoint
# Runs as root by design: the control-plane process manages bind-mounted host
# directories and files on behalf of arbitrary database owner UIDs/GIDs (chown,
# permission setup for Postgres/Patroni data dirs and certs), which requires
# root privileges. Distroless static has no shell/su-exec to safely drop
# privileges after that setup either. Trivy's DS-0002 (non-root user) is
# suppressed repo-wide in .trivyignore since inline ignore comments don't work
# for this whole-file check.
ENTRYPOINT ["/pgedge-control-plane"]

ADD pgedge-control-plane_${ARCHIVE_VERSION}_${TARGETOS}_${TARGETARCH}.tar.gz /
