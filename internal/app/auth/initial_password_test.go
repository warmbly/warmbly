package auth

import (
	"testing"

	"github.com/warmbly/warmbly/internal/app/appattest"
)

// Only an app build that does not declare the change step is told to finish on the web.
func TestAppCannotChangePassword(t *testing.T) {
	const appUA = "Warmbly/412 CFNetwork/3826.500.131 Darwin/25.0.0"
	const safariUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
	proof := &appattest.Proof{KeyID: "k", Assertion: "a", Challenge: "c"}
	cases := []struct {
		name string
		data AuthData
		ua   string
		want bool
	}{
		{"older app by user agent", AuthData{}, appUA, true},
		{"older app by app attest", AuthData{AppAttest: proof}, "", true},
		{"current app", AuthData{SupportsPasswordChange: true}, appUA, false},
		{"current app with app attest", AuthData{AppAttest: proof, SupportsPasswordChange: true}, appUA, false},
		{"browser", AuthData{}, safariUA, false},
		{"api client", AuthData{}, "curl/8.7.1", false},
		{"another app on the same stack", AuthData{}, "OtherApp/3 CFNetwork/3826.500.131 Darwin/25.0.0", false},
	}
	for _, tc := range cases {
		if got := appCannotChangePassword(&tc.data, tc.ua); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
