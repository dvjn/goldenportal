FROM gcr.io/distroless/static-debian13:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88

ARG TARGETARCH
COPY --chmod=0555 dist/${TARGETARCH}/goldport /usr/local/bin/goldport

USER 65532:65532
EXPOSE 8000 2121

ARG VERSION=dev
ENV APP_NAME=goldport \
    APP_VERSION=${VERSION} \
    HTTP_PORT=8000 \
    METRICS_PORT=2121 \
    GOFR_TELEMETRY=false

ENTRYPOINT ["/usr/local/bin/goldport"]
