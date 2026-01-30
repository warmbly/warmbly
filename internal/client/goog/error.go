package goog

import (
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"google.golang.org/api/googleapi"
)

func HandleError(err error) *errx.MailError {
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

	return nil
}
