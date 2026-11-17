package api

import (
	"strings"
	"testing"
)

func TestRenderMarkdownKeepsGitHubLayout(t *testing.T) {
	src := "<div align=\"center\">\n\n" +
		"<img src=\"https://example.com/logo.svg\" alt=\"logo\" width=\"104\" />\n\n" +
		"# doreg\n\n" +
		"[![CI](https://example.com/ci.svg)](https://example.com/ci)\n\n" +
		"</div>\n\n" +
		"<details><summary>More</summary>\n\nHidden\n\n</details>\n"
	out := renderMarkdown(src)
	for _, want := range []string{
		`<div align="center">`,
		`<img src="https://example.com/logo.svg" alt="logo" width="104"`,
		`<h1 id="doreg">doreg</h1>`,
		`<img src="https://example.com/ci.svg" alt="CI"`,
		`<details>`, `<summary>More</summary>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
}

func TestRenderMarkdownStripsScript(t *testing.T) {
	src := "<script>alert(1)</script>\n\n" +
		"<img src=x onerror=alert(1)>\n\n" +
		"<a href=\"javascript:alert(1)\">a</a> [b](javascript:alert(2))\n\n" +
		"<p style=\"position:fixed\" align=\"javascript:x\" onclick=\"x()\">p</p>\n\n" +
		"<iframe src=\"https://example.com\"></iframe><form action=\"/api/logout\"><button>x</button></form>\n"
	out := renderMarkdown(src)
	for _, bad := range []string{"<script", "onerror", "javascript:", "style=", "onclick", "align=", "<iframe", "<form"} {
		if strings.Contains(out, bad) {
			t.Errorf("output contains %s:\n%s", bad, out)
		}
	}
}

func TestRenderMarkdownGitHubHeadingIDs(t *testing.T) {
	src := "## 🚀 Quick start\n\n## 🛠️ Development\n\n## What you get\n\n## What you get\n\n## C++ & Go!\n"
	out := renderMarkdown(src)
	for _, want := range []string{
		`id="-quick-start"`,
		"id=\"\ufe0f-development\"",
		`id="what-you-get"`,
		`id="what-you-get-1"`,
		`id="c--go"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
}
