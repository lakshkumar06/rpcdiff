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
.TIMEOUT, .INVALID_RESPONSE, .INCONCLUSIVE { background: #fde8e8; }
code { font-size: 12px; }
.meta { color: #444; margin-bottom: 1.5rem; }
</style>
</head>
<body>
`)
	b.WriteString("<h1>rpcdiff</h1>\n<div class=\"meta\">")
	b.WriteString("<div>Baseline: <code>" + html.EscapeString(run.Baseline) + "</code></div>")
	b.WriteString("<div>Candidate: <code>" + html.EscapeString(run.Candidate) + "</code></div>")
	b.WriteString("<div>Time: " + html.EscapeString(run.Timestamp.Format("2006-01-02 15:04:05 UTC")) + "</div>")
	b.WriteString("<div>Requests: " + strconv.Itoa(run.Summary.Total) + " &nbsp; Matches: " + strconv.Itoa(run.Summary.Matches) + " &nbsp; Failures: " + strconv.Itoa(run.Summary.Failures) + "</div>")
	b.WriteString("</div>\n<p>" + html.EscapeString(run.Disclaimer) + "</p>\n")
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
	}
	b.WriteString("</table>\n</body></html>\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
