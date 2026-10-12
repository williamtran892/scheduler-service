package platform

import (
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfig_DefaultsWhenNothingIsSet(t *testing.T) {
	cfg, err := LoadConfig(envOf(nil))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := Config{Addr: ":8080", DSN: DefaultDSN, ServiceName: "scheduler", OTelExporter: ExporterNone, ShutdownTimeout: 10 * time.Second}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadConfig_ReadsEveryVariable(t *testing.T) {
	cfg, err := LoadConfig(envOf(map[string]string{
		"HTTP_ADDR":         ":9000",
		"MYSQL_DSN":         "u:p@tcp(db:3306)/x",
		"OTEL_SERVICE_NAME": "sched-test",
		"OTEL_EXPORTER":     ExporterOTLP,
		"SHUTDOWN_TIMEOUT":  "3s",
	}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := Config{Addr: ":9000", DSN: "u:p@tcp(db:3306)/x", ServiceName: "sched-test", OTelExporter: ExporterOTLP, ShutdownTimeout: 3 * time.Second}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadConfig_RejectsInvalidValues(t *testing.T) {
	tests := map[string]map[string]string{
		"unknown exporter":   {"OTEL_EXPORTER": "zipkin"},
		"unparsable timeout": {"SHUTDOWN_TIMEOUT": "soon"},
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(envOf(env)); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}
