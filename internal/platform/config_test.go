package platform

import (
	"strings"
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
	want := Config{Addr: ":8080", DSN: DefaultDSN, ServiceName: "scheduler", OTelExporter: ExporterNone, ShutdownTimeout: 10 * time.Second,
		NoShowGrace: 30 * time.Minute, NoShowSweepInterval: time.Minute}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadConfig_ReadsEveryVariable(t *testing.T) {
	cfg, err := LoadConfig(envOf(map[string]string{
		"HTTP_ADDR":              ":9000",
		"MYSQL_DSN":              "u:p@tcp(db:3306)/x",
		"OTEL_SERVICE_NAME":      "sched-test",
		"OTEL_EXPORTER":          ExporterOTLP,
		"SHUTDOWN_TIMEOUT":       "3s",
		"NO_SHOW_GRACE":          "45m",
		"NO_SHOW_SWEEP_INTERVAL": "1s",
		"CORS_ALLOWED_ORIGINS":   "*",
	}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := Config{Addr: ":9000", DSN: "u:p@tcp(db:3306)/x", ServiceName: "sched-test", OTelExporter: ExporterOTLP, ShutdownTimeout: 3 * time.Second, CORSOrigins: "*",
		NoShowGrace: 45 * time.Minute, NoShowSweepInterval: time.Second}
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

// The no-show sweeper's grace period and interval must be positive durations (research R7).
func TestLoadConfig_RejectsANoShowGraceOrIntervalThatIsNotAPositiveDuration(t *testing.T) {
	for _, variable := range []string{"NO_SHOW_GRACE", "NO_SHOW_SWEEP_INTERVAL"} {
		for _, value := range []string{"soon", "0s", "-1m"} {
			t.Run(variable+"="+value, func(t *testing.T) {
				_, err := LoadConfig(envOf(map[string]string{variable: value}))
				if err == nil || !strings.Contains(err.Error(), variable) {
					t.Errorf("error = %v, want one naming %s", err, variable)
				}
			})
		}
	}
}
