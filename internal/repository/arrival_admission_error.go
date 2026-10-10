package repository

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

type arrivalAdmissionError struct {
	stage string
	cause error
}

func (e *arrivalAdmissionError) Error() string { return "arrival admission: " + e.stage }
func (e *arrivalAdmissionError) Unwrap() error { return e.cause }

func arrivalFailure(stage string, err error) error {
	return &arrivalAdmissionError{stage: stage, cause: err}
}

// ArrivalAdmissionDiagnostic exposes only server-controlled categories and SQLSTATE, never error text.
func ArrivalAdmissionDiagnostic(err error) (string, string) {
	stage := "dependency"
	var failure *arrivalAdmissionError
	if errors.As(err, &failure) {
		stage = failure.stage
	} else if errors.Is(err, ErrArrivalOutboxUnsupported) {
		stage = "unsupported"
	} else if errors.Is(err, ErrArrivalAdmissionUnconfirmed) {
		stage = "durability_unconfirmed"
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && len(pg.Code) == 5 {
		for _, c := range pg.Code {
			if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z') {
				return stage, ""
			}
		}
		return stage, pg.Code
	}
	return stage, ""
}
