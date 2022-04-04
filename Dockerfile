FROM golang:1.18 AS build
WORKDIR /build
COPY . .
RUN go build -o /ue-exporter ./cmd/exporter/main.go && go build -o /oneshot ./cmd/oneshot/main.go
FROM gcr.io/distroless/base
COPY --from=build /ue-exporter /bin/ue-exporter
COPY --from=build /oneshot /bin/oneshot
ENTRYPOINT /bin/ue-exporter