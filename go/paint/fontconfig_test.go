package paint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"browser-lite/paint"
)

func TestCacheFontFileRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	font, err := os.ReadFile(filepath.Join("..", "..", "test", "data", "typography-webfont-test.ttf"))
	if err != nil {
		t.Skipf("fixture font: %v", err)
	}
	url := "http://example.com/fonts/test.ttf"
	first, ok := paint.CacheFontFile(url, font)
	if !ok {
		t.Fatalf("CacheFontFile failed")
	}
	if !strings.HasSuffix(first, ".ttf") {
		t.Errorf("cache path %q keeps the ttf extension", first)
	}
	back, err := os.ReadFile(first)
	if err != nil || string(back) != string(font) {
		t.Errorf("cached bytes differ: %v", err)
	}
	second, ok := paint.CacheFontFile(url, font)
	if !ok || second != first {
		t.Errorf("same URL re-caches to the same path: %q vs %q", first, second)
	}
	otf, ok := paint.CacheFontFile("http://example.com/f.otf", font)
	if !ok || !strings.HasSuffix(otf, ".otf") {
		t.Errorf("otf URL keeps the otf extension: %q", otf)
	}
}

func TestRegisterFontFile(t *testing.T) {
	font := filepath.Join("..", "..", "test", "data", "typography-webfont-test.ttf")
	abs, err := filepath.Abs(font)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("fixture font: %v", err)
	}
	if !paint.RegisterFontFile(abs) {
		t.Errorf("fontconfig rejected the fixture font")
	}
	if paint.RegisterFontFile(filepath.Join(t.TempDir(), "missing.ttf")) {
		t.Errorf("fontconfig accepted a missing file")
	}
}
