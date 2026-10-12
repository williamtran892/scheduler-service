// Package platform holds the cross-cutting pieces every domain uses: configuration,
// clock, id generation, the MySQL pool, the problem-details error encoder, health
// and OpenTelemetry setup. It must not import any domain package.
package platform

import (
	"fmt"
	"time"
)

// DefaultDSN points at the MySQL started by docker-compose.yml. parseTime and loc=UTC make
// every DATETIME a UTC time.Time; time_zone='+00:00' pins the session zone (plan section 5.2).
const DefaultDSN = "scheduler:scheduler@tcp(127.0.0.1:3306)/scheduler?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27&charset=utf8mb4"

// Exporter values for OTEL_EXPORTER.
const (
	ExporterNone   = "none"
	ExporterStdout = "stdout"
	ExporterOTLP   = "otlp"
)

// Config is everything the process reads from its environment. See README for each variable.
type Config struct {
	Addr            string        // HTTP_ADDR
	DSN             string        // MYSQL_DSN
	ServiceName     string        // OTEL_SERVICE_NAME
	OTelExporter    string        // OTEL_EXPORTER: none, stdout or otlp
	ShutdownTimeout time.Duration // SHUTDOWN_TIMEOUT
}

// LoadConfig builds a Config from getenv (os.Getenv in main, a map in tests),
// applying defaults for anything unset and rejecting values it cannot parse.
func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:            orDefault(getenv("HTTP_ADDR"), ":8080"),
		DSN:             orDefault(getenv("MYSQL_DSN"), DefaultDSN),
		ServiceName:     orDefault(getenv("OTEL_SERVICE_NAME"), "scheduler"),
		OTelExporter:    orDefault(getenv("OTEL_EXPORTER"), ExporterNone),
		ShutdownTimeout: 10 * time.Second,
	}

	switch cfg.OTelExporter {
	case ExporterNone, ExporterStdout, ExporterOTLP:
	default:
		return Config{}, fmt.Errorf("OTEL_EXPORTER: unknown value %q (want none, stdout or otlp)", cfg.OTelExporter)
	}

	if v := getenv("SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err)
		}
		cfg.ShutdownTimeout = d
	}

	return cfg, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
