package telemetry

import (
	"testing"
	"time"
)

func TestEnabled(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	if Enabled() {
		t.Fatal("Enabled() = true without an endpoint")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://example.invalid/otlp")
	if !Enabled() {
		t.Fatal("Enabled() = false with an endpoint")
	}
	t.Setenv("OTEL_SDK_DISABLED", "true")
	if Enabled() {
		t.Fatal("Enabled() = true when SDK is disabled")
	}
}

func TestMetricInterval(t *testing.T) {
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "15000")
	if got := metricInterval(); got != 15*time.Second {
		t.Fatalf("metricInterval() = %s, want 15s", got)
	}
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "invalid")
	if got := metricInterval(); got != 60*time.Second {
		t.Fatalf("metricInterval() = %s, want 60s", got)
	}
}
