FROM docker.io/library/golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /mock .

FROM scratch
COPY --from=build /mock /mock
COPY mock.yaml /etc/mock/
COPY payloads /etc/mock/payloads
ENTRYPOINT ["/mock", "/etc/mock/mock.yaml"]
