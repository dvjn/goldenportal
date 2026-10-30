FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99

ARG TARGETARCH
COPY --chmod=0555 dist/${TARGETARCH}/goldenportal /usr/local/bin/goldenportal

USER 65532:65532
EXPOSE 8000 2121

ARG VERSION=dev
ENV APP_NAME=goldenportal \
    APP_VERSION=${VERSION} \
    HTTP_PORT=8000 \
    METRICS_PORT=2121 \
    GOFR_TELEMETRY=false

ENTRYPOINT ["/usr/local/bin/goldenportal"]
