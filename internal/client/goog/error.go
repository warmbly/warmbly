package goog

import (
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"google.golang.org/api/googleapi"
)

func HandleError(err error) *errx.MailError {
	if err == nil {
		return nil
	}
	if gerr, ok := err.(*googleapi.Error); ok {
		switch gerr.Code {
		case 401:
			return errx.ErrMailGoogleAuth
		case 402:
			return errx.ErrMailGooglePayment
		case 403:
			return errx.ErrMailGoogleForbidden(gerr.Message)
		default:
			respErr := errx.ErrMailGoogleUnknown(gerr.Code, gerr.Message)
			log.Debug().Err(err).Msg("Google Api Error")
			return respErr
		}
	}

	// Non-API failures (DNS, TLS, timeouts) are transient transport errors.
	// Returning nil here would silently swallow them and leave callers holding
	// a typed-nil *MailError in an error interface.
	log.Debug().Err(err).Msg("Google transport error")
	return errx.ErrMailServerUnreachable
}
