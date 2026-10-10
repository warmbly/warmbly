package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestArrivalAdmissionDiagnosticNeverExposesErrorText(t *testing.T) {
	private := "private-provider-message-and-encryption-material"
	for _, tc := range []struct {
		err          error
		stage, state string
	}{
		{errors.New(private), "dependency", ""},
		{fmt.Errorf("%s: %w", private, ErrArrivalOutboxUnsupported), "unsupported", ""},
		{fmt.Errorf("%s: %w", private, ErrArrivalAdmissionUnconfirmed), "durability_unconfirmed", ""},
		{arrivalFailure("organization_key", errors.New(private)), "organization_key", ""},
		{arrivalFailure("mapping_write", &pgconn.PgError{Code: "22021", Message: private, Detail: private, Where: private}), "mapping_write", "22021"},
		{arrivalFailure("mapping_write", &pgconn.PgError{Code: private, Message: private}), "mapping_write", ""},
		{arrivalFailure("mapping_write", &pgconn.PgError{Code: "a!bcd", Message: private}), "mapping_write", ""},
	} {
		stage, state := ArrivalAdmissionDiagnostic(tc.err)
		if stage != tc.stage || state != tc.state || strings.Contains(stage+state, private) {
			t.Fatalf("unexpected safe diagnostic: %q %q", stage, state)
		}
	}
}

func TestArrivalOwnershipLossRequiresAuthoritativeAbsence(t *testing.T) {
	for _, stage := range []string{"mailbox_ownership", "mailbox_ownership_recheck"} {
		for _, cause := range []error{pgx.ErrNoRows, fmt.Errorf("wrapped: %w", pgx.ErrNoRows), context.Canceled, context.DeadlineExceeded, errors.New("database unavailable"), &pgconn.PgError{Code: "08006"}} {
			err := arrivalOwnershipFailure(stage, cause)
			if errors.Is(err, ErrArrivalMailboxOwnershipLost) != errors.Is(cause, pgx.ErrNoRows) || !errors.Is(err, cause) {
				t.Fatalf("ownership error incorrectly classified: %v", cause)
			}
			if got, _ := ArrivalAdmissionDiagnostic(err); got != stage {
				t.Fatalf("lost stage: %s", got)
			}
		}
	}
	if errors.Is(arrivalFailure("organization_key", pgx.ErrNoRows), ErrArrivalMailboxOwnershipLost) {
		t.Fatal("unrelated missing row retired mailbox")
	}
}

func TestArrivalAdmissionFailurePreservesCauseAndHidesText(t *testing.T) {
	cause := errors.New("private payload")
	err := arrivalFailure("payload_encoding", cause)
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "private payload") {
		t.Fatal("failure lost its cause or exposed private text")
	}
	r := &pgEmailMessageMapRepository{}
	if stage, _ := ArrivalAdmissionDiagnostic(r.AdmitArrival(t.Context(), EmailMessageData{}, nil)); stage != "validation" {
		t.Fatalf("missing validation category: %s", stage)
	}
}
