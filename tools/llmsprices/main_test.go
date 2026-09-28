package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAmountsReadsEveryWayConohaWritesAYenAmount(t *testing.T) {
	got := amounts("3,608\n円 424.0円 1,386 円 13.2円 100GB 2026年 無料")
	want := []float64{3608, 424, 1386, 13.2}
	if len(got) != len(want) {
		t.Fatalf("amounts = %v, want %v", got, want)
	}
	for _, v := range want {
		if !got[v] {
			t.Errorf("amounts lacks %v: %v", v, got)
		}
	}
}

func TestRunPassesWhenEveryAmountIsPublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><script>var x = "9,999円";</script><p><strong>3,608</strong><wbr>円</p><p>424.0&nbsp;円</p></html>`))
	}))
	defer srv.Close()
	file := writeFile(t, "| 4GB | 3,608円 |\n| 追加 IP | 424円 |\n")
	var out bytes.Buffer
	if err := run(t.Context(), file, []source{{url: srv.URL}}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "(2 distinct)") {
		t.Errorf("out = %q", out.String())
	}
}

func TestRunNamesTheAmountsThatVanished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<p>3,700円</p>`))
	}))
	defer srv.Close()
	file := writeFile(t, "3,608円 and 1.2円 and 3,700円\n")
	err := run(t.Context(), file, []source{{url: srv.URL}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "2 amounts") || !strings.Contains(err.Error(), "1.2円 3608円") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunReadsPDFsThroughPdftotext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("%PDF-1.4 stand-in"))
	}))
	defer srv.Close()
	pdfText = func(_ context.Context, path string) ([]byte, error) {
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
		return []byte("1時間  1.2円  1.8円"), nil
	}
	t.Setenv("PATH", t.TempDir())
	file := writeFile(t, "| 512MB | 1.2円 |\n")
	err := run(t.Context(), file, []source{{url: srv.URL, pdf: true}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "pdftotext is not on PATH") {
		t.Fatalf("err without pdftotext = %v", err)
	}
	fake := filepath.Join(t.TempDir(), "pdftotext")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(fake))
	if err := run(t.Context(), file, []source{{url: srv.URL, pdf: true}}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "llms.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
