// Command llmsprices checks that every yen amount written in llms.txt still
// appears on ConoHa's pricing pages and spec sheets, so a price change on
// ConoHa's side shows up as a missing amount instead of going unnoticed.
//
// It needs pdftotext (poppler) on PATH to read the spec sheets, which are the
// only source of the hourly rates.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A source is one page or PDF that ConoHa publishes prices on.
type source struct {
	url string
	pdf bool
}

var conohaSources = []source{
	{url: "https://vps.conoha.jp/pricing/"},
	{url: "https://vps.conoha.jp/windows/pricing/"},
	{url: "https://vps.conoha.jp/pdf/conoha_spec_ja.pdf", pdf: true},
	{url: "https://vps.conoha.jp/windows/pdf/conoha_ws_spec_ja.pdf", pdf: true},
	{url: "https://vps.conoha.jp/pdf/vswj020-old/conoha_spec_ja.pdf", pdf: true},
}

var (
	yen     = regexp.MustCompile(`(\d[\d,]*(?:\.\d+)?)[\s\p{Zs}]*円`)
	tags    = regexp.MustCompile(`(?s)<(script|style)\b.*?</(script|style)>|<[^>]+>`)
	pdfText = func(ctx context.Context, path string) ([]byte, error) {
		return exec.CommandContext(ctx, "pdftotext", "-layout", path, "-").Output()
	}
)

func main() {
	file := flag.String("f", "llms.txt", "the file whose yen amounts are checked")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := run(ctx, *file, conohaSources, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, file string, sources []source, out io.Writer) error {
	written, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if needsPDF(sources) {
		if _, err := exec.LookPath("pdftotext"); err != nil {
			return errors.New("pdftotext is not on PATH (brew install poppler); it reads the spec sheets that hold the hourly rates")
		}
	}
	published := map[float64]bool{}
	for _, s := range sources {
		text, err := s.text(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", s.url, err)
		}
		for a := range amounts(text) {
			published[a] = true
		}
	}
	var missing []string
	for a := range amounts(string(written)) {
		if !published[a] {
			missing = append(missing, strconv.FormatFloat(a, 'f', -1, 64)+"円")
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%d amounts in %s no longer appear on ConoHa's pages or spec sheets: %s", len(missing), file, strings.Join(missing, " "))
	}
	fmt.Fprintf(out, "every amount in %s (%d distinct) still appears on ConoHa's pages or spec sheets\n", file, len(amounts(string(written))))
	return nil
}

func needsPDF(sources []source) bool {
	for _, s := range sources {
		if s.pdf {
			return true
		}
	}
	return false
}

// text fetches the source and returns it as plain text.
func (s source) text(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	if !s.pdf {
		return html.UnescapeString(tags.ReplaceAllString(string(body), " ")), nil
	}
	f, err := os.CreateTemp("", "llmsprices-*.pdf")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(body); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	text, err := pdfText(ctx, f.Name())
	if err != nil {
		return "", fmt.Errorf("pdftotext: %w", err)
	}
	return string(text), nil
}

// amounts returns every yen amount in the text as a number, so 424円, 424.0円
// and 1,386 円 all count as the same price.
func amounts(text string) map[float64]bool {
	found := map[float64]bool{}
	for _, m := range yen.FindAllStringSubmatch(text, -1) {
		v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err == nil {
			found[v] = true
		}
	}
	return found
}
