# Prometheus Exporter for Flatcar Linux Update Engine Status

This is a small Prometheus Exporter for the Flatcar Linux Update Engine status,
it provides a HTTP interface which returns the status of various aspects of the
Flatcar Linux update engine.

This is built into a Docker image, which can be installed using the supplied
manifests (DaemonSet and Prometheus rules/monitor).
