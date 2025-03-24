FROM golang:1.24.2-alpine AS builder

WORKDIR /build

ENV CGO_ENABLED=0

COPY --from=src . .
ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target="/root/.cache/go-build" go build -o ./control-plane .

FROM scratch

COPY --from=builder /build/control-plane /control-plane

CMD [ "/control-plane" ]
