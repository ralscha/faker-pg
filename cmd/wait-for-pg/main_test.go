package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRejectsInvalidConfigurationBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{
		{"-dsn", "host=localhost password='super-secret"},
		{"-dsn", "postgres://user:super-secret@localhost:bad/db"},
		{"-timeout", "0s"}, {"-timeout", "-1s"}, {"unexpected"},
	} {
		var output bytes.Buffer
		if code := run(args, &output); code == 0 {
			t.Fatalf("run(%q) unexpectedly succeeded", args)
		}
		if strings.Contains(output.String(), "super-secret") || strings.Contains(output.String(), "Waiting for") {
			t.Fatal("invalid configuration leaked credentials or started waiting")
		}
	}
}

func TestRunHelp(t *testing.T) {
	var output bytes.Buffer
	if code := run([]string{"-help"}, &output); code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	if !strings.Contains(output.String(), "-timeout") {
		t.Fatalf("help output = %q", output.String())
	}
}

func TestConnectionLabelDoesNotExposeCredentials(t *testing.T) {
	label := connectionLabel("postgres://user:super-secret@db.example.com:6543/app?sslmode=require")
	if strings.Contains(label, "user") || strings.Contains(label, "super-secret") {
		t.Fatalf("connection label leaked credentials: %q", label)
	}
	if label != "db.example.com:6543/app" {
		t.Fatalf("connection label = %q", label)
	}
}

func TestConnectionLabelHandlesInvalidDSN(t *testing.T) {
	if label := connectionLabel("://not a dsn"); label != "the configured server" {
		t.Fatalf("connection label = %q", label)
	}
}
