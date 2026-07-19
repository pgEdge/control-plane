FROM debian:12

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl && \
    rm -rf /var/lib/apt/lists/*

COPY ./pgedge-control-plane /pgedge-control-plane

# nosemgrep: dockerfile.security.missing-user-entrypoint.missing-user-entrypoint
# trivy:ignore:DS-0002 -- CI-only image; runs as root by design, same
# reasoning as the production Dockerfile (bind-mount ownership management).
ENTRYPOINT ["/pgedge-control-plane"]
