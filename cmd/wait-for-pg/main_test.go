package main

import (
	"strings"
	"testing"
)

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
