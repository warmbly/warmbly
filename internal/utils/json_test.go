package utils

import "testing"

// Issue #207: this rule used to be ^[A-Za-z0-9_]+$, so a CSV column called
// "Company Mobile" failed every row of the import even though the campaign
// template engine has always been able to resolve a spaced key. The set below
// is exactly what tmplvars.Normalize can address, and the same rule
// is mirrored in web/src/components/app/contacts/importShared.ts.
func TestIsValidJSONKey(t *testing.T) {
	valid := []string{"role", "Job_Title", "Company Mobile", "first-name", "plan tier 2", "a"}
	for _, k := range valid {
		if !IsValidJSONKey(k) {
			t.Errorf("IsValidJSONKey(%q) = false, want true", k)
		}
	}
	invalid := []string{
		"", " ", "Company/Mobile", "Revenue ($)", "a.b", "тест",
		" leading", "trailing ", "-dash", "dash-", "a\tb",
	}
	for _, k := range invalid {
		if IsValidJSONKey(k) {
			t.Errorf("IsValidJSONKey(%q) = true, want false", k)
		}
	}
	long := make([]byte, MaxJSONKeyLength+1)
	for i := range long {
		long[i] = 'a'
	}
	if IsValidJSONKey(string(long)) {
		t.Error("an over-length key was accepted")
	}
}

func TestNormalizeJSONKey(t *testing.T) {
	cases := map[string]string{
		"  Company   Mobile ": "Company Mobile",
		"Company\tMobile":     "Company Mobile",
		"role":                "role",
		"   ":                 "",
	}
	for in, want := range cases {
		if got := NormalizeJSONKey(in); got != want {
			t.Errorf("NormalizeJSONKey(%q) = %q, want %q", in, got, want)
		}
	}
	// Normalizing is what makes a header with stray whitespace usable.
	if !IsValidJSONKey(NormalizeJSONKey(" Company  Mobile ")) {
		t.Error("a normalized header should be a valid key")
	}
}
