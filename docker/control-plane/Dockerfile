FROM scratch

ARG TARGETOS
ARG TARGETARCH

ENTRYPOINT ["/control-plane"]

COPY --from=dist controlplane_${TARGETOS}_${TARGETARCH}*/control-plane /
