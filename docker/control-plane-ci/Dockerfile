FROM debian:12

RUN apt-get update && \
    apt-get install -y curl && \
    rm -rf /var/lib/apt/lists/*

COPY ./pgedge-control-plane /control-plane

ENTRYPOINT ["/pgedge-control-plane"]
