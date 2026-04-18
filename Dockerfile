FROM gcr.io/distroless/static:nonroot
ARG TARGETARCH
WORKDIR /
COPY manager-linux-${TARGETARCH} manager
USER 65532:65532

ENTRYPOINT ["/manager"]
