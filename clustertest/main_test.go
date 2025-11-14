//go:build cluster_test

package clustertest

import (
	"flag"
	"log"
	"os"
	"testing"

	"github.com/docker/docker/pkg/ioutils"
	"github.com/testcontainers/testcontainers-go"
)

const defaultImageTag = "127.0.0.1:5000/control-plane:latest"

var testConfig = struct {
	skipCleanup    bool
	skipImageBuild bool
	imageTag       string
	dataDirPrefix  string
	dataDir        string
}{}

func TestMain(m *testing.M) {
	flag.BoolVar(&testConfig.skipCleanup, "skip-cleanup", false, "skip cleaning up resources created by the tests")
	flag.BoolVar(&testConfig.skipImageBuild, "skip-image-build", false, "skip building the control plane image. this setting is implied true when a non-default image-tag is specified.")
	flag.StringVar(&testConfig.imageTag, "image-tag", defaultImageTag, "the control plane image to test")
	flag.StringVar(&testConfig.dataDirPrefix, "data-dir", "", "the directory to store test data. defaults to clustertest/data")

	flag.Parse()

	if !testConfig.skipImageBuild && testConfig.imageTag == defaultImageTag {
		buildImage()
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
