// Package report renders Markdown reports to standalone HTML and then to PDF
// via a configurable headless Chromium or wkhtmltopdf binary. When no PDF
// binary is available the HTML is returned so the platform still produces a
// real, downloadable artifact.
package report

import (
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Renderer struct {
	binary   string
	renderer string // chromium | wkhtmltopdf
	tmpDir   string
}

func NewRenderer(binary, renderer, tmpDir string) *Renderer {
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	_ = os.MkdirAll(tmpDir, 0o755)
	return &Renderer{binary: binary, renderer: renderer, tmpDir: tmpDir}
}

// HTML wraps rendered Markdown in the Vault-themed print stylesheet.
func (r *Renderer) HTML(title, bodyMarkdown string, meta map[string]string) string {
	var metaRows strings.Builder
	for k, v := range meta {
		metaRows.WriteString(fmt.Sprintf("<tr><th>%s</th><td>%s</td></tr>", html.EscapeString(k), html.EscapeString(v)))
	}
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>%s</title>
<style>%s</style></head><body>
<header><div class="brand">CyberRange OS</div><h1>%s</h1>
<table class="meta">%s</table></header>
<main>%s</main>
<footer>Generated %s — Institution-hosted, confidential training evidence.</footer>
</body></html>`,
		html.EscapeString(title), printCSS, html.EscapeString(title), metaRows.String(),
		MarkdownToHTML(bodyMarkdown), time.Now().Format("2006-01-02 15:04"))
}

// ToPDF renders the HTML to a PDF file and returns its bytes. If no binary is
// configured/available, returns the HTML bytes with ok=false.
func (r *Renderer) ToPDF(ctx context.Context, htmlContent string) ([]byte, string, error) {
	if r.binary == "" || !binaryAvailable(r.binary) {
		return []byte(htmlContent), "text/html", nil
	}
	base := filepath.Join(r.tmpDir, fmt.Sprintf("report-%d", time.Now().UnixNano()))
	htmlPath := base + ".html"
	pdfPath := base + ".pdf"
	if err := os.WriteFile(htmlPath, []byte(htmlContent), 0o600); err != nil {
		return nil, "", err
	}
	defer os.Remove(htmlPath)
	defer os.Remove(pdfPath)

	var cmd *exec.Cmd
	if r.renderer == "wkhtmltopdf" {
		cmd = exec.CommandContext(ctx, r.binary, "--enable-local-file-access", htmlPath, pdfPath)
	} else {
		cmd = exec.CommandContext(ctx, r.binary, "--headless", "--disable-gpu", "--no-sandbox",
			"--print-to-pdf="+pdfPath, "file://"+htmlPath)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("pdf render failed: %v: %s", err, string(out))
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, "", err
	}
	return data, "application/pdf", nil
}

func binaryAvailable(bin string) bool {
	if strings.ContainsAny(bin, "/\\") {
		_, err := os.Stat(bin)
		return err == nil
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// MarkdownToHTML is a compact CommonMark-subset renderer (headings, bold,
// italic, code, code fences, lists, links, tables-as-text, paragraphs). It is
// intentionally dependency-free.
func MarkdownToHTML(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out strings.Builder
	inCode := false
	inList := false
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inCode {
				out.WriteString("</code></pre>")
				inCode = false
			} else {
				out.WriteString("<pre><code>")
				inCode = true
			}
			continue
		}
		if inCode {
			out.WriteString(html.EscapeString(line) + "\n")
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			if !inList {
				out.WriteString("<ul>")
				inList = true
			}
			out.WriteString("<li>" + inline(trimmed[2:]) + "</li>")
			continue
		}
		if inList {
			out.WriteString("</ul>")
			inList = false
		}
		switch {
		case strings.HasPrefix(trimmed, "### "):
			out.WriteString("<h3>" + inline(trimmed[4:]) + "</h3>")
		case strings.HasPrefix(trimmed, "## "):
			out.WriteString("<h2>" + inline(trimmed[3:]) + "</h2>")
		case strings.HasPrefix(trimmed, "# "):
			out.WriteString("<h1>" + inline(trimmed[2:]) + "</h1>")
		default:
			out.WriteString("<p>" + inline(trimmed) + "</p>")
		}
	}
	if inList {
		out.WriteString("</ul>")
	}
	if inCode {
		out.WriteString("</code></pre>")
	}
	return out.String()
}

var (
	reBold   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic = regexp.MustCompile(`\*(.+?)\*`)
	reCode   = regexp.MustCompile("`(.+?)`")
	reLink   = regexp.MustCompile(`\[(.+?)\]\((.+?)\)`)
)

func inline(s string) string {
	s = html.EscapeString(s)
	s = reCode.ReplaceAllString(s, "<code>$1</code>")
	s = reBold.ReplaceAllString(s, "<strong>$1</strong>")
	s = reItalic.ReplaceAllString(s, "<em>$1</em>")
	s = reLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	return s
}

const printCSS = `
body{font-family:'Inter',Arial,sans-serif;color:#17171A;margin:40px;line-height:1.55}
header{border-bottom:2px solid #D4AF37;margin-bottom:24px;padding-bottom:12px}
.brand{color:#D4AF37;font-weight:700;letter-spacing:2px;text-transform:uppercase;font-size:12px}
h1{font-size:24px;margin:6px 0}h2{font-size:18px;margin-top:22px;border-bottom:1px solid #eee;padding-bottom:4px}
h3{font-size:15px}
table.meta{font-size:12px;border-collapse:collapse;margin-top:8px}
table.meta th{text-align:left;color:#666;padding-right:12px;font-weight:600}
pre{background:#0A0A0B;color:#F5F3EE;padding:12px;border-radius:6px;overflow:auto;font-family:'JetBrains Mono',monospace;font-size:12px}
code{font-family:'JetBrains Mono',monospace;background:#f2f2f2;padding:1px 4px;border-radius:3px}
pre code{background:none;padding:0}
footer{margin-top:40px;border-top:1px solid #ddd;padding-top:8px;color:#999;font-size:10px}
a{color:#8B0000}
`
