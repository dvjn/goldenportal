FROM docker.io/library/golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
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
