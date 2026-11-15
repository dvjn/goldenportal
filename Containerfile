FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931

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
