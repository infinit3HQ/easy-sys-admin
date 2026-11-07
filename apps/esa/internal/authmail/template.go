package authmail

import (
	"bytes"
	"html/template"
)

// Letter is one auth message: the words, the button, and the link.
type Letter struct {
	Heading   string
	Paragraph string
	Button    string
	Fine      string
	Link      string
}

// PlainText is the copy clients get when they do not show HTML.
func PlainText(l Letter) string {
	return l.Heading + "\n\n" + l.Paragraph + "\n\n" + l.Link + "\n\n" + l.Fine + "\n"
}

var letterTmpl = template.Must(template.New("letter").Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Heading}}</title>
</head>
<body style="margin:0;padding:0;background:#0b0d0c;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#0b0d0c;">
<tr><td align="center" style="padding:24px 12px;">
<table role="presentation" width="560" cellpadding="0" cellspacing="0" style="max-width:560px;width:100%;background:#0b0d0c;border:1px solid #232925;border-radius:8px;">
<tr><td style="padding:22px 28px 0;font-family:Arial Narrow,Helvetica Neue,sans-serif;font-size:18px;font-weight:700;letter-spacing:0.08em;text-transform:uppercase;color:#ece8dc;">
<span style="display:inline-block;width:10px;height:10px;background:#ffb000;margin-right:10px;"></span>Easy Sys Admin
</td></tr>
<tr><td style="padding:22px 28px 28px;font-family:Segoe UI,Helvetica,Arial,sans-serif;">
<h1 style="margin:0 0 10px;font-family:Arial Narrow,Helvetica Neue,sans-serif;font-size:34px;line-height:0.95;font-weight:700;letter-spacing:0.02em;text-transform:uppercase;color:#ece8dc;">{{.Heading}}</h1>
<p style="margin:0;font-size:15px;line-height:1.5;color:#bdb9ad;">{{.Paragraph}}</p>
<p style="margin:22px 0 0;">
<a href="{{.Link}}" style="display:inline-block;padding:12px 18px;background:#ffb000;color:#0b0d0c;border-radius:6px;font-size:14px;font-weight:500;text-decoration:none;">{{.Button}}</a>
</p>
<p style="margin:22px 0 0;font-size:13px;line-height:1.5;color:#7c8279;">{{.Fine}}</p>
<p style="margin:14px 0 0;font-family:ui-monospace,monospace;font-size:12px;line-height:1.45;color:#7c8279;word-break:break-all;">{{.Link}}</p>
</td></tr>
<tr><td style="padding:14px 28px 16px;border-top:1px solid #232925;font-family:Segoe UI,Helvetica,Arial,sans-serif;font-size:12px;color:#4c534d;">Easy Sys Admin</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))

// HTML renders the message the way the product looks: a dark panel and an
// amber button with the same 6px corners as the site.
func HTML(l Letter) (string, error) {
	var buf bytes.Buffer
	if err := letterTmpl.Execute(&buf, l); err != nil {
		return "", err
	}
	return buf.String(), nil
}
