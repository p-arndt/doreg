package api

import "testing"

func TestParseGitHubSource(t *testing.T) {
	tests := []struct {
		in    string
		owner string
		repo  string
		ok    bool
	}{
		{"https://github.com/acme/tool", "acme", "tool", true},
		{"https://github.com/acme/tool.git", "acme", "tool", true},
		{"https://github.com/acme/tool/", "acme", "tool", true},
		{"https://github.com/acme/tool/tree/main/sub", "acme", "tool", true},
		{"https://github.com/acme/tool?tab=readme#top", "acme", "tool", true},
		{"https://github.com/acme/my.app", "acme", "my.app", true},
		{"github.com/acme/tool", "acme", "tool", true},
		{"acme/tool", "acme", "tool", true},
		{"  acme/tool  ", "acme", "tool", true},

		{"", "", "", false},
		{"http://github.com/acme/tool", "", "", false},
		{"ftp://github.com/acme/tool", "", "", false},
		{"https://gitlab.com/acme/tool", "", "", false},
		{"gitlab.com/acme/tool", "", "", false},
		{"https://github.com@evil.com/acme/tool", "", "", false},
		{"https://user@github.com/acme/tool", "", "", false},
		{"https://github.com:443/acme/tool", "", "", false},
		{"https://github.com.evil.com/acme/tool", "", "", false},
		{"https://github.com/../x", "", "", false},
		{"https://github.com/acme/..", "", "", false},
		{"https://github.com/acme/.", "", "", false},
		{"https://github.com/./x", "", "", false},
		{"https://github.com/acme", "", "", false},
		{"https://github.com/acme/.git", "", "", false},
		{"https://github.com/", "", "", false},
		{"acme/tool name", "", "", false},
		{"acme tool/x", "", "", false},
	}
	for _, tt := range tests {
		owner, repo, ok := parseGitHubSource(tt.in)
		if ok != tt.ok || owner != tt.owner || repo != tt.repo {
			t.Errorf("parseGitHubSource(%q) = (%q, %q, %t), want (%q, %q, %t)",
				tt.in, owner, repo, ok, tt.owner, tt.repo, tt.ok)
		}
	}
}

func TestRewriteRelativeLinks(t *testing.T) {
	in := "![logo](docs/logo.png)\n" +
		"![dot](./img/a.png \"Title\")\n" +
		"![slash](/img/b.png)\n" +
		"[guide](./docs/guide.md 'Guide')\n" +
		"[abs](https://example.com/x)\n" +
		"[mail](mailto:a@example.com)\n" +
		"[anchor](#usage)\n" +
		"[proto](//cdn.example.com/x.js)\n" +
		"<img src=\"img/c.png\" alt=\"c\">\n" +
		"<a href=\"CONTRIBUTING.md\">contributing</a>\n" +
		"<img src=\"https://example.com/d.png\">\n" +
		"<img src=\"assets/Logo.SVG\" width=\"104\">\n"
	want := "![logo](https://raw.githubusercontent.com/acme/tool/HEAD/docs/logo.png)\n" +
		"![dot](https://raw.githubusercontent.com/acme/tool/HEAD/img/a.png \"Title\")\n" +
		"![slash](https://raw.githubusercontent.com/acme/tool/HEAD/img/b.png)\n" +
		"[guide](https://github.com/acme/tool/blob/HEAD/docs/guide.md 'Guide')\n" +
		"[abs](https://example.com/x)\n" +
		"[mail](mailto:a@example.com)\n" +
		"[anchor](#usage)\n" +
		"[proto](//cdn.example.com/x.js)\n" +
		"<img src=\"https://raw.githubusercontent.com/acme/tool/HEAD/img/c.png\" alt=\"c\">\n" +
		"<a href=\"https://github.com/acme/tool/blob/HEAD/CONTRIBUTING.md\">contributing</a>\n" +
		"<img src=\"https://example.com/d.png\">\n" +
		"<img src=\"https://raw.githubusercontent.com/acme/tool/HEAD/assets/Logo.SVG?sanitize=true\" width=\"104\">\n"
	if got := rewriteRelativeLinks(in, "acme", "tool"); got != want {
		t.Errorf("rewriteRelativeLinks mismatch\n got: %s\nwant: %s", got, want)
	}
}
