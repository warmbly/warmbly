package repository

import (
	"errors"
	"testing"
)

func TestLiveAuthenticatorStepRequiresActiveEnrollment(t *testing.T) {
	f := newTransportFixture(t)
	repo := NewTOTPRepository(f.pool)
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO user_totp_settings(user_id,totp_secret_sealed,totp_enabled) VALUES($1,'test-sealed',true)`, f.user); err != nil {
		t.Fatal(err)
	}
	if fresh, err := repo.ConsumeTOTPStep(t.Context(), f.user, 100, "test-sealed"); !fresh || err != nil {
		t.Fatalf("first step: fresh=%t err=%v", fresh, err)
	}
	if fresh, err := repo.ConsumeTOTPStep(t.Context(), f.user, 100, "test-sealed"); fresh || err != nil {
		t.Fatalf("used step: fresh=%t err=%v", fresh, err)
	}
	if fresh, err := repo.ConsumeTOTPStep(t.Context(), f.user, 99, "test-sealed"); fresh || err != nil {
		t.Fatalf("superseded step: fresh=%t err=%v", fresh, err)
	}
	if fresh, err := repo.ConsumeTOTPStep(t.Context(), f.user, 101, "previous-sealed"); fresh || !errors.Is(err, ErrTOTPEnrollmentChanged) {
		t.Fatalf("enrollment binding: fresh=%t err=%v", fresh, err)
	}
	type result struct {
		fresh bool
		err   error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() {
			fresh, err := repo.ConsumeTOTPStep(t.Context(), f.user, 101, "test-sealed")
			results <- result{fresh, err}
		}()
	}
	accepted := 0
	for range 8 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.fresh {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("concurrent step accepted %d times, want once", accepted)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE user_totp_settings SET totp_enabled=false WHERE user_id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	if fresh, err := repo.ConsumeTOTPStep(t.Context(), f.user, 102, "test-sealed"); fresh || !errors.Is(err, ErrTOTPEnrollmentChanged) {
		t.Fatalf("inactive enrollment: fresh=%t err=%v", fresh, err)
	}
	if _, err := f.pool.Exec(t.Context(), `DELETE FROM user_totp_settings WHERE user_id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	if fresh, err := repo.ConsumeTOTPStep(t.Context(), f.user, 102, "test-sealed"); fresh || !errors.Is(err, ErrTOTPEnrollmentChanged) {
		t.Fatalf("missing enrollment: fresh=%t err=%v", fresh, err)
	}
}
