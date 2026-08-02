package jsonata

import "testing"

func TestEval(t *testing.T) {
	t.Run("sha256 extension", func(t *testing.T) {
		v, err := Eval("$sha256('hi')", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// sha256("hi")
		if v != "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4" {
			t.Fatalf("unexpected sha256: %v", v)
		}
	})

	t.Run("base64 extension", func(t *testing.T) {
		v, err := Eval("$base64encode('hello')", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != "aGVsbG8=" {
			t.Fatalf("unexpected base64: %v", v)
		}
	})

	t.Run("hmac extension", func(t *testing.T) {
		_, err := Eval("$hmac('k','m','sha256')", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("object navigation", func(t *testing.T) {
		v, err := Eval("$.a.b", map[string]any{"a": map[string]any{"b": 42}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != 42 {
			t.Fatalf("unexpected value: %v", v)
		}
	})

	t.Run("invalid expression", func(t *testing.T) {
		if err := Parse("$bad["); err == nil {
			t.Fatal("expected parse error")
		}
	})

	t.Run("valid expression", func(t *testing.T) {
		if err := Parse("$uppercase(name)"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
