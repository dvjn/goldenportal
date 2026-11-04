FROM docker.io/library/golang:1.27.3-alpine AS build
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
