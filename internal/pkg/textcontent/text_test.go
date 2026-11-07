package textcontent

import "testing"

func TestSupportedText(t *testing.T) {
	for _, s := range []string{"", "\ufeffname\tcity\r\nRenée 東京\n", "Καλημέρα\fnext page"} {
		if err := Validate([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []string{"nul\x00", "escape\x1b", "invalid\xff", "control\u0085"} {
		if err := Validate([]byte(s)); err == nil {
			t.Fatal("accepted binary controls or invalid UTF-8")
		}
	}
}
