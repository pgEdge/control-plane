//go:build cluster_test

package clustertest

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/docker/docker/pkg/ioutils"
	"github.com/testcontainers/testcontainers-go"
)

var testConfig = struct {
	skipCleanup    bool
	skipImageBuild bool
	imageTag       string
	dataDirPrefix  string
	dataDir        string
}{}

// resolveVersion returns the CONTROL_PLANE_VERSION from the environment, falling
// back to `git describe` when the env var is unset or empty (which can happen
// when the common.mk lazy-evaluation fails in CI).
func resolveVersion() string {
	if v := os.Getenv("CONTROL_PLANE_VERSION"); v != "" {
		return v
	}
	out, err := exec.Command("git", "-C", "..", "describe", "--tags", "--abbrev=0", "--match", "v*").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func TestMain(m *testing.M) {
	version := resolveVersion()
	defaultImageTag := "127.0.0.1:5000/control-plane:" + version

	flag.BoolVar(&testConfig.skipCleanup, "skip-cleanup", false, "skip cleaning up resources created by the tests")
	flag.BoolVar(&testConfig.skipImageBuild, "skip-image-build", false, "skip building the control plane image. this setting is implied true when a non-default image-tag is specified.")
	flag.StringVar(&testConfig.imageTag, "image-tag", defaultImageTag, "the control plane image to test")
	flag.StringVar(&testConfig.dataDirPrefix, "data-dir", "", "the directory to store test data. defaults to clustertest/data")

	flag.Parse()

	if !testConfig.skipImageBuild && testConfig.imageTag == defaultImageTag {
		buildImage(version)
	} else {
		log.Println("skipping image build")
	}

	if err := makeDataDir(); err != nil {
		log.Fatal(err)
	}

	log.Printf("skip cleanup: %t", testConfig.skipCleanup)

	// Disable testcontainers logging
	testcontainers.Logger = log.New(&ioutils.NopWriter{}, "", 0)

	if testConfig.skipCleanup {
		// There's no programmatic interface for this setting outside of this
		// environment variable.
		os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	}

	code := m.Run()

	cleanupDataDir()

	os.Exit(code)
}
