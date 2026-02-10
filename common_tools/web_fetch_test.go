package common_tools

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

func mockHTTPGet(body string, status int) func(string) (*http.Response, error) {
	return func(url string) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(bytes.NewBufferString(body)),
		}, nil
	}
}

func TestWebFetchMarkdown(t *testing.T) {
	orig := httpGet
	defer func() { httpGet = orig }()

	httpGet = mockHTTPGet(`<html><body><h1>Hello World</h1><p>This is a <b>test</b> paragraph.</p><a href="https://example.com">link</a></body></html>`, 200)

	result, err := Web_Fetch("https://example.com", "markdown", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "# Hello World") {
		t.Errorf("expected markdown header, got %q", result)
	}
	if !strings.Contains(result, "**test**") {
		t.Errorf("expected bold, got %q", result)
	}
	if !strings.Contains(result, "[link](https://example.com)") {
		t.Errorf("expected link, got %q", result)
	}
}

func TestWebFetchText(t *testing.T) {
	orig := httpGet
	defer func() { httpGet = orig }()

	httpGet = mockHTTPGet(`<html><body>
		<script>var x = 1;</script>
		<style>body { color: red; }</style>
		<p>Clean text here.</p>
	</body></html>`, 200)

	result, err := Web_Fetch("https://example.com", "text", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Clean text here.") {
		t.Errorf("expected clean text, got %q", result)
	}
	if strings.Contains(result, "var x") {
		t.Error("script content should be stripped")
	}
	if strings.Contains(result, "color: red") {
		t.Error("style content should be stripped")
	}
}

func TestWebFetchMaxChars(t *testing.T) {
	orig := httpGet
	defer func() { httpGet = orig }()

	httpGet = mockHTTPGet("<p>"+strings.Repeat("a", 1000)+"</p>", 200)

	result, err := Web_Fetch("https://example.com", "text", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) > 120 { // 100 + truncation message
		t.Errorf("expected truncated result, got len %d", len(result))
	}
	if !strings.Contains(result, "truncated") {
		t.Error("expected truncation notice")
	}
}

func TestWebFetchEmptyURL(t *testing.T) {
	_, err := Web_Fetch("", "markdown", 0)
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestWebFetchHTTPError(t *testing.T) {
	orig := httpGet
	defer func() { httpGet = orig }()

	httpGet = mockHTTPGet("not found", 404)

	_, err := Web_Fetch("https://example.com/missing", "text", 0)
	if err == nil {
		t.Error("expected error for 404")
	}
}

func TestHTMLToMarkdownEntities(t *testing.T) {
	html := "<p>&amp; &lt; &gt; &quot; &#39;</p>"
	result := htmlToMarkdown(html)
	if !strings.Contains(result, "& < > \" '") {
		t.Errorf("entities not decoded: %q", result)
	}
}

func TestHTMLToMarkdownLists(t *testing.T) {
	html := "<ul><li>one</li><li>two</li></ul>"
	result := htmlToMarkdown(html)
	if !strings.Contains(result, "- one") || !strings.Contains(result, "- two") {
		t.Errorf("list not converted: %q", result)
	}
}

func TestHTMLToText(t *testing.T) {
	html := "<p>Hello <b>world</b></p>"
	result := htmlToText(html)
	if !strings.Contains(result, "Hello world") {
		t.Errorf("expected plain text, got %q", result)
	}
}
