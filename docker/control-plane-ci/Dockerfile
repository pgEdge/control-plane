FROM debian:12

RUN apt-get update && \
    apt-get install -y curl && \
    rm -rf /var/lib/apt/lists/*

COPY ./control-plane /control-plane

ENTRYPOINT ["/control-plane"]
