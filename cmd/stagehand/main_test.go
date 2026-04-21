package main

import (
	"bytes"
	"testing"
)

func TestRun(t *testing.T) {
	var out bytes.Buffer

	if err := run(&out); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	want := "stagehand CLI scaffold 0.1.0-alpha.0: implementation starts in later stories\n"
	if out.String() != want {
		t.Fatalf("run() output = %q, want %q", out.String(), want)
	}
}
