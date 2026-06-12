package main

import (
	"errors"
	"testing"
)

func TestRunTile(t *testing.T) {
	if err := runTile(func() error { panic("boom") }); err == nil {
		t.Error("runTile should convert a panic into an error, got nil")
	}
	sentinel := errors.New("write failed")
	if err := runTile(func() error { return sentinel }); err != sentinel {
		t.Errorf("runTile should pass the error through, got %v", err)
	}
	if err := runTile(func() error { return nil }); err != nil {
		t.Errorf("runTile(success) = %v, want nil", err)
	}
}
