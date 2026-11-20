package auth

import (
	"regexp"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// An emailed code is six random digits.
func TestVerificationCodeIsSixDigits(t *testing.T) {
	six := regexp.MustCompile(`^[0-9]{6}$`)
	seen := map[string]bool{}
	for range 2000 {
		code, err := crypt.VerificationCode()
		if err != nil || !six.MatchString(code) {
			t.Fatalf("code %q, err %v", code, err)
		}
		seen[code] = true
	}
	if len(seen) < 1990 {
		t.Fatalf("%d distinct codes in 2000 draws", len(seen))
	}
}
