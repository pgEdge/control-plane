FROM debian:12

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl && \
    rm -rf /var/lib/apt/lists/*

COPY ./pgedge-control-plane /pgedge-control-plane

# nosemgrep: dockerfile.security.missing-user-entrypoint.missing-user-entrypoint
# CI-only image; runs as root by design, same reasoning as the production
# Dockerfile (bind-mount ownership management). Trivy's DS-0002 is suppressed
# repo-wide in .trivyignore since inline ignore comments don't work for this
# whole-file check.
ENTRYPOINT ["/pgedge-control-plane"]
