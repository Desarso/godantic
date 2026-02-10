package common_tools

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Web_Fetch fetches a URL and extracts readable content as markdown/text.
// extractMode can be "markdown" or "text". maxChars limits output length (0 = no limit).
func Web_Fetch(url string, extractMode string, maxChars int) (string, error) {
	if url == "" {
		return "", fmt.Errorf("url is required")
	}
	if extractMode == "" {
		extractMode = "markdown"
	}

	resp, err := httpGet(url)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB limit
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	html := string(body)
	var result string
	if extractMode == "text" {
		result = htmlToText(html)
	} else {
		result = htmlToMarkdown(html)
	}

	if maxChars > 0 && len(result) > maxChars {
		result = result[:maxChars] + "\n...(truncated)"
	}

	return result, nil
}

// httpGet is a package-level var so tests can mock it.
var httpGet = defaultHTTPGet

func defaultHTTPGet(url string) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FastClaw/1.0 (Web Fetch Tool)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain")
	return client.Do(req)
}

// htmlToText strips all HTML tags and returns plain text.
func htmlToText(html string) string {
	// Remove script and style blocks
	reScript := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = reScript.ReplaceAllString(html, "")
	html = reStyle.ReplaceAllString(html, "")

	// Remove tags
	reTag := regexp.MustCompile(`<[^>]+>`)
	text := reTag.ReplaceAllString(html, "")

	// Decode common entities
	text = decodeEntities(text)

	// Collapse whitespace
	reSpaces := regexp.MustCompile(`[ \t]+`)
	text = reSpaces.ReplaceAllString(text, " ")

	// Collapse multiple newlines
	reNewlines := regexp.MustCompile(`\n{3,}`)
	text = reNewlines.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// htmlToMarkdown does a basic HTML-to-markdown conversion.
func htmlToMarkdown(html string) string {
	// Remove script and style blocks
	reScript := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = reScript.ReplaceAllString(html, "")
	html = reStyle.ReplaceAllString(html, "")

	// Headers
	for i := 6; i >= 1; i-- {
		re := regexp.MustCompile(fmt.Sprintf(`(?is)<h%d[^>]*>(.*?)</h%d>`, i, i))
		prefix := strings.Repeat("#", i)
		html = re.ReplaceAllString(html, "\n"+prefix+" $1\n")
	}

	// Paragraphs and divs → newlines
	rePara := regexp.MustCompile(`(?is)<(?:p|div)[^>]*>`)
	html = rePara.ReplaceAllString(html, "\n")
	reParaClose := regexp.MustCompile(`(?is)</(?:p|div)>`)
	html = reParaClose.ReplaceAllString(html, "\n")

	// Line breaks
	reBr := regexp.MustCompile(`(?i)<br\s*/?>`)
	html = reBr.ReplaceAllString(html, "\n")

	// Inline formatting BEFORE stripping other tags
	// Bold
	reBoldB := regexp.MustCompile(`(?is)<b(?:\s[^>]*)?>(.+?)</b>`)
	html = reBoldB.ReplaceAllString(html, "**$1**")
	reBoldStrong := regexp.MustCompile(`(?is)<strong(?:\s[^>]*)?>(.+?)</strong>`)
	html = reBoldStrong.ReplaceAllString(html, "**$1**")

	// Italic
	reItalicI := regexp.MustCompile(`(?is)<i(?:\s[^>]*)?>(.+?)</i>`)
	html = reItalicI.ReplaceAllString(html, "*$1*")
	reItalicEm := regexp.MustCompile(`(?is)<em(?:\s[^>]*)?>(.+?)</em>`)
	html = reItalicEm.ReplaceAllString(html, "*$1*")

	// Code
	reCode := regexp.MustCompile(`(?is)<code[^>]*>(.*?)</code>`)
	html = reCode.ReplaceAllString(html, "`$1`")

	// Links
	reLink := regexp.MustCompile(`(?is)<a[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	html = reLink.ReplaceAllString(html, "[$2]($1)")

	// List items
	reLi := regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`)
	html = reLi.ReplaceAllString(html, "- $1\n")

	// Strip ALL remaining tags (html, head, body, nav, footer, etc.)
	reTag := regexp.MustCompile(`<[^>]+>`)
	html = reTag.ReplaceAllString(html, "")

	// Decode entities
	html = decodeEntities(html)

	// Collapse whitespace
	reSpaces := regexp.MustCompile(`[ \t]+`)
	html = reSpaces.ReplaceAllString(html, " ")

	reNewlines := regexp.MustCompile(`\n{3,}`)
	html = reNewlines.ReplaceAllString(html, "\n\n")

	return strings.TrimSpace(html)
}

func decodeEntities(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&apos;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return s
}
