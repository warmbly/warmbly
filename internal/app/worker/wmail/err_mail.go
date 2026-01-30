package wmail

import (
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

var sleepTimes []time.Duration = []time.Duration{
	10 * time.Second,
	1 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

func (w *WMail) RunMailError(f func() *errx.MailError) {
	for i := range 5 {
		err := f()
		if err == nil {
			return
		}

		switch err.ResolveMethod {
		case errx.MailErrorResolveMethodRetry:
			time.Sleep(sleepTimes[i])
			continue
		case errx.MailErrorResolveMethodAuth:
		case errx.MailErrorResolveMethodReload:
			w.Terminate()
		}

		return
	}
}

func (w *WMail) Terminate() {
	w.Cancel()
	w.TerminateFunc()
}
