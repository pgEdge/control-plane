//go:build e2e_test

package e2e

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
)

var fixture *TestFixture

func TestMain(m *testing.M) {
	var fixtureName string
	var skipCleanup bool

	flag.StringVar(&fixtureName, "fixture", "", "the name of the test fixture to use")
	flag.BoolVar(&skipCleanup, "skip-cleanup", false, "skip cleaning up test fixtures")
	flag.Parse()

	var config TestConfig

	if fixtureName != "" {
		raw, err := os.ReadFile(fmt.Sprintf("./fixtures/outputs/%s.test_config.yaml", fixtureName))
		if err != nil {
			log.Fatal(err)
		}
		if err := yaml.Unmarshal(raw, &config); err != nil {
			log.Fatal(err)
		}
	} else {
		config = DefaultTestConfig()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	f, err := NewTestFixture(ctx, config, skipCleanup)
	if err != nil {
		log.Fatal(err)
	}

	fixture = f

	// Run tests
	os.Exit(m.Run())
}

func pointerTo[T any](v T) *T {
	return &v
}
