package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEffectiveToken(t *testing.T) {
	t.Run("inline token wins over file", func(t *testing.T) {
		got, err := effectiveToken(commonFlags{token: "inline", tokenFile: "/no/such/file"})
		if err != nil || got != "inline" {
			t.Fatalf("got %q, %v; want inline, nil", got, err)
		}
	})

	t.Run("from file, trimmed", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "tok")
		if err := os.WriteFile(p, []byte("  file-token\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := effectiveToken(commonFlags{tokenFile: p})
		if err != nil || got != "file-token" {
			t.Fatalf("got %q, %v; want file-token, nil", got, err)
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		if _, err := effectiveToken(commonFlags{tokenFile: "/no/such/file"}); err == nil {
			t.Fatal("expected an error for a missing --token-file")
		}
	})

	t.Run("none configured returns empty", func(t *testing.T) {
		got, err := effectiveToken(commonFlags{})
		if err != nil || got != "" {
			t.Fatalf("got %q, %v; want empty, nil", got, err)
		}
	})
}

func TestEnvBool(t *testing.T) {
	cases := []struct {
		val      string
		fallback bool
		want     bool
	}{
		{"true", false, true},
		{"1", false, true},
		{"yes", false, true},
		{"false", true, false},
		{"0", true, false},
		{"no", true, false},
		{"", true, true},
		{"garbage", true, true},
		{"garbage", false, false},
	}
	for _, tc := range cases {
		t.Setenv("MACHINERY_TEST_BOOL", tc.val)
		if got := envBool("MACHINERY_TEST_BOOL", tc.fallback); got != tc.want {
			t.Errorf("envBool(%q, %v) = %v, want %v", tc.val, tc.fallback, got, tc.want)
		}
	}
}

func TestTrunc(t *testing.T) {
	if got := trunc("hello", 10); got != "hello" {
		t.Errorf("trunc short string: got %q, want unchanged", got)
	}
	if got := trunc("abcdefghij", 5); len([]rune(got)) != 5 {
		t.Errorf("trunc(10 chars, 5) = %q (%d runes), want 5", got, len([]rune(got)))
	}
}
