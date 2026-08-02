package config

import (
	"errors"
	"testing"
)

func TestLoad_MissingEncKey(t *testing.T) {
	t.Setenv("KS_ENC_KEY", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when KS_ENC_KEY is missing")
	}
	if !errors.Is(err, ErrMissingEncKey) {
		t.Fatalf("expected ErrMissingEncKey, got %v", err)
	}
}

func TestLoad_WithEncKey(t *testing.T) {
	t.Setenv("KS_ENC_KEY", "test-key-32-bytes--for-config-test")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(cfg.EncKey) != "test-key-32-bytes--for-config-test" {
		t.Fatalf("expected test key, got %s", cfg.EncKey)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("expected default addr :8080, got %s", cfg.Addr)
	}
}
