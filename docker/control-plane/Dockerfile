FROM gcr.io/distroless/static-debian13:latest@sha256:15f8637509792e05b97d83451b0b6ccfa4abd548f997e406ac1e72c87fe1fa78

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
