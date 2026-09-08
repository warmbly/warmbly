package jobs

import (
	"context"
	"time"

	emailverifyapp "github.com/warmbly/warmbly/internal/app/emailverify"
	"github.com/warmbly/warmbly/internal/jobrun"
)

// DeliveryEvidenceJob turns campaign sends that never bounced into
// verification evidence: a delivery the recipient's server kept is the
// strongest proof the mailbox exists, and it costs nothing to observe.
type DeliveryEvidenceJob struct {
	evidence *emailverifyapp.Evidence
	interval time.Duration
	batch    int
}

func NewDeliveryEvidenceJob(evidence *emailverifyapp.Evidence, interval time.Duration, batch int) *DeliveryEvidenceJob {
	if batch <= 0 {
		batch = 2000
	}
	return &DeliveryEvidenceJob{evidence: evidence, interval: interval, batch: batch}
}

// Start runs the job on its interval until ctx ends. A full batch repeats
// at once so a backlog drains.
func (j *DeliveryEvidenceJob) Start(ctx context.Context) {
	jobrun.Loop(ctx, "delivery_evidence", j.interval, false, func(ctx context.Context) error {
		for {
			n, err := j.evidence.CreditCleanDeliveries(ctx, j.batch)
			if err != nil {
				return err
			}
			if n < j.batch || ctx.Err() != nil {
				return nil
			}
		}
	})
}
