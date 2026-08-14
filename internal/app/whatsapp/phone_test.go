package whatsapp

import "testing"

func TestNormalizePhoneBR(t *testing.T) {
	cases := []struct {
		raw  string
		want string // E.164
		ok   bool
	}{
		{"(48) 99999-9999", "+5548999999999", true},
		{"48999999999", "+5548999999999", true},
		{"+55 48 99999-9999", "+5548999999999", true},
		{"5548999999999", "+5548999999999", true},
		{"048 99999-9999", "+5548999999999", true},
		{"", "", false},
		{"123", "", false},
		{"not-a-phone", "", false},
	}
	for _, tc := range cases {
		got := NormalizePhone(tc.raw, "BR")
		if got.Valid != tc.ok {
			t.Errorf("raw=%q valid=%v want %v err=%s", tc.raw, got.Valid, tc.ok, got.Error)
			continue
		}
		if tc.ok && got.E164 != tc.want {
			t.Errorf("raw=%q e164=%q want %q", tc.raw, got.E164, tc.want)
		}
		if got.Original != tc.raw {
			t.Errorf("original not preserved: %q", got.Original)
		}
	}
}

func TestDigitsOnly(t *testing.T) {
	if DigitsOnly("+55 48 99999-9999") != "5548999999999" {
		t.Fatal(DigitsOnly("+55 48 99999-9999"))
	}
}
