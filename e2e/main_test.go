//go:build e2e_test

package e2e

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
)

var fixture *TestFixture

func defaultDebugDir() string {
	return filepath.Join(".", "debug", time.Now().Format("20060102150405"))
}

func TestMain(m *testing.M) {
	var fixtureName string
	var skipCleanup bool
	var debug bool
	var debugDir string

	flag.StringVar(&fixtureName, "fixture", "", "the name of the test fixture to use")
	flag.BoolVar(&skipCleanup, "skip-cleanup", false, "skip cleaning up test fixtures")
	flag.BoolVar(&debug, "debug", false, "write debugging information on failures")
	flag.StringVar(&debugDir, "debug-dir", defaultDebugDir(), "directory to write debug output")
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

	f, err := NewTestFixture(ctx, config, skipCleanup, debug, debugDir)
	if err != nil {
		log.Fatal(err)
	}

	fixture = f

	// Run tests
	start := time.Now()
	code := m.Run()

	if code != 0 && debug {
		debugWriteControlPlaneInfo(debugDir, start)
	}

	os.Exit(code)
}

func pointerTo[T any](v T) *T {
	return &v
}
