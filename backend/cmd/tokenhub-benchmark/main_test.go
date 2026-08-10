package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpListsBenchmarkCommands(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := run(t.Context(), []string{"help"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"mocker", "gateway", "run", "check"} {
		if !strings.Contains(output.String(), command) {
			t.Fatalf("help does not list %q: %s", command, output.String())
		}
	}
}

func TestGatewayRequiresBenchmarkKeyFromEnvironment(t *testing.T) {
	t.Setenv("TOKENHUB_BENCHMARK_API_KEY", "")

	var output bytes.Buffer
	err := run(t.Context(), []string{"gateway"}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "TOKENHUB_BENCHMARK_API_KEY") {
		t.Fatalf("expected missing benchmark key error, got %v", err)
	}
}
