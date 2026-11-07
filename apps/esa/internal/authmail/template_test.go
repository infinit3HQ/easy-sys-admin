package authmail

import (
	"strings"
	"testing"
)

func TestHTMLButtonIsRounded(t *testing.T) {
	html, err := HTML(Letter{
		Heading:   "Confirm your email",
		Paragraph: "Confirm this address.",
		Button:    "Confirm email",
		Fine:      "The link works for 24 hours.",
		Link:      "https://easysysadmin.app/login?mode=confirm&token=abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "border-radius:6px") {
		t.Fatal("button is not rounded")
	}
	if !strings.Contains(html, "href=\"https://easysysadmin.app/login?mode=confirm&amp;token=abc\"") {
		t.Fatal("link was not escaped into the button")
	}
	plain := PlainText(Letter{Heading: "Confirm your email", Paragraph: "Confirm this address.", Link: "https://easysysadmin.app/login?mode=confirm&token=abc", Fine: "The link works for 24 hours."})
	if !strings.Contains(plain, "token=abc") {
		t.Fatal(plain)
	}
}
