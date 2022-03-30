FROM golang:1.18 AS build
WORKDIR /build
COPY . .
RUN go build -o /ue-exporter ./cmd/exporter/main.go
FROM gcr.io/distroless/base
COPY --from=build /ue-exporter /bin/ue-exporter
ENTRYPOINT /bin/ue-exporter