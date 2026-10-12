// Package acceptance runs the Gherkin features in /features against the running service at
// SCHEDULER_BASE_URL. It is a black-box client: it reaches the service only through its HTTP API
// and has no access to the database. scripts/integration.sh starts the service and a fresh
// MySQL in containers, runs this package, and tears both down:
//
//	make test-integration
//	make test-integration TAGS=@bootstrap      (sets GODOG_TAGS)
package acceptance

import (
	"context"
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

// suiteOptions configures godog. GODOG_TAGS filters scenarios (e.g. "@bootstrap", "~@slow");
// GODOG_FORMAT picks the formatter (default "pretty"; CI uses "junit:<path>").
func suiteOptions(t *testing.T) *godog.Options {
	format := os.Getenv("GODOG_FORMAT")
	if format == "" {
		format = "pretty"
	}
	return &godog.Options{
		Output:      colors.Colored(os.Stdout),
		Format:      format,
		Tags:        os.Getenv("GODOG_TAGS"),
		Paths:       []string{"../../features"},
		Strict:      true, // an undefined or pending step fails the run
		Concurrency: 1,    // scenarios share one service and one database
		TestingT:    t,
	}
}

func TestFeatures(t *testing.T) {
	ctx := context.Background()
	target, err := newTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}

	suite := godog.TestSuite{
		Name:                "scheduler",
		ScenarioInitializer: func(sc *godog.ScenarioContext) { initScenario(sc, target) },
		Options:             suiteOptions(t),
	}
	if suite.Run() != 0 {
		t.Fatal("acceptance suite failed")
	}
}

// initScenario gives every scenario a fresh world and registers the step definitions against it.
// The database is not reset between scenarios: each run starts from a fresh database, and each
// scenario creates or picks the data it needs so it does not depend on what others left behind.
func initScenario(sc *godog.ScenarioContext, t *target) {
	w := newWorld(t)
	registerHTTPSteps(sc, w)
	registerBookingSteps(sc, w)
	registerRecordSteps(sc, w)
	registerRaceSteps(sc, w)
}
