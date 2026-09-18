package report

import (
	"html"
	"os"
	"strconv"
	"strings"
)

func WriteHTML(path string, run Run) error {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>rpcdiff report</title>
<style>
body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 2rem; color: #111; }
table { border-collapse: collapse; width: 100%; font-size: 14px; }
th, td { border: 1px solid #ddd; padding: 6px 8px; vertical-align: top; }
th { background: #f4f4f4; text-align: left; }
.MATCH { background: #e8f7e8; }
.VALUE_MISMATCH, .ERROR_MISMATCH, .SHAPE_MISMATCH { background: #fff4e0; }
.TIMEOUT, .TRANSIENT_FAILURE, .INVALID_RESPONSE, .INCONCLUSIVE { background: #fde8e8; }
code { font-size: 12px; }
.meta { color: #444; margin-bottom: 1.5rem; }
.pass { color: #176b2c; font-weight: 700; }
.fail { color: #a12622; font-weight: 700; }
ul { margin-top: .35rem; }
</style>
</head>
<body>
`)
	b.WriteString("<h1>rpcdiff</h1>\n<div class=\"meta\">")
	b.WriteString("<div>Baseline: <code>" + html.EscapeString(run.Baseline) + "</code></div>")
	b.WriteString("<div>Candidate: <code>" + html.EscapeString(run.Candidate) + "</code></div>")
	if run.Proxy != "" {
		b.WriteString("<div>Proxy: <code>" + html.EscapeString(run.Proxy) + "</code></div>")
	}
	b.WriteString("<div>Time: " + html.EscapeString(run.Timestamp.Format("2006-01-02 15:04:05 UTC")) + "</div>")
	b.WriteString("<div>Requests: " + strconv.Itoa(run.Summary.Total) + " &nbsp; Matches: " + strconv.Itoa(run.Summary.Matches) + " &nbsp; Compatibility mismatches: " + strconv.Itoa(run.Summary.CompatibilityMismatches) + " &nbsp; Transport failures: " + strconv.Itoa(run.Summary.TransportFailures) + " &nbsp; Skipped: " + strconv.Itoa(run.Summary.Skipped) + "</div>")
	statusClass, status := "pass", "COMPATIBLE"
	if run.Summary.CompatibilityMismatches > 0 || run.Summary.TransportFailures > 0 {
		statusClass, status = "fail", "INCOMPATIBLE"
	}
	b.WriteString("<div class=\"" + statusClass + "\">Gate status: " + status + "</div>")
	b.WriteString("</div>\n<p>" + html.EscapeString(run.Disclaimer) + "</p>\n")
	b.WriteString("<h2>Compatibility summary</h2>\n<table><tr><th>Category</th><th>Count</th></tr>\n")
	for _, category := range []struct {
		label string
		count int
	}{
		{label: "Matches", count: run.Summary.Matches},
		{label: "Compatibility mismatches", count: run.Summary.CompatibilityMismatches},
		{label: "Transport failures", count: run.Summary.TransportFailures},
		{label: "Skipped", count: run.Summary.Skipped},
	} {
		if category.count > 0 {
			b.WriteString("<tr><td>" + category.label + "</td><td>" + strconv.Itoa(category.count) + "</td></tr>\n")
		}
	}
	b.WriteString("</table>\n<h3>By classification</h3>\n<table><tr><th>Classification</th><th>Count</th></tr>\n")
	for _, class := range []string{"MATCH", "VALUE_MISMATCH", "SHAPE_MISMATCH", "ERROR_MISMATCH", "TIMEOUT", "TRANSIENT_FAILURE", "INVALID_RESPONSE", "INCONCLUSIVE", "SKIPPED"} {
		if count := run.Summary.ByCategory[class]; count > 0 {
			b.WriteString("<tr><td>" + class + "</td><td>" + strconv.Itoa(count) + "</td></tr>\n")
		}
	}
	b.WriteString("</table>\n")
	b.WriteString("<h2>Results</h2>\n<table>\n<tr><th>#</th><th>Method</th><th>Class</th><th>Path</th><th>Baseline ms</th><th>Candidate ms</th></tr>\n")
	for i, r := range run.Results {
		cls := string(r.Classification)
		b.WriteString("<tr class=\"" + cls + "\">")
		b.WriteString("<td>" + strconv.Itoa(i) + "</td>")
		b.WriteString("<td><code>" + html.EscapeString(r.Method) + "</code></td>")
		b.WriteString("<td>" + html.EscapeString(cls) + "</td>")
		b.WriteString("<td><code>" + html.EscapeString(r.DifferencePath) + "</code></td>")
		b.WriteString("<td>" + strconv.FormatFloat(r.Baseline.LatencyMS, 'f', 2, 64) + "</td>")
		b.WriteString("<td>" + strconv.FormatFloat(r.Candidate.LatencyMS, 'f', 2, 64) + "</td>")
		b.WriteString("</tr>\n")
		if len(r.Diffs) > 0 || len(r.Notes) > 0 {
			b.WriteString("<tr><td></td><td colspan=\"5\"><ul>")
			for _, d := range r.Diffs {
				b.WriteString("<li><code>" + html.EscapeString(d.Path) + "</code>: " + html.EscapeString(d.Message) + "</li>")
			}
			for _, note := range r.Notes {
				b.WriteString("<li>" + html.EscapeString(note) + "</li>")
			}
			b.WriteString("</ul></td></tr>\n")
		}
	}
	b.WriteString("</table>\n</body></html>\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
