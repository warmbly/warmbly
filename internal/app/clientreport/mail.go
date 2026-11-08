package clientreport

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// emailCampaignRows bounds the campaign summary inside an email; the page has the rest.
const emailCampaignRows = 5

// deliver sends one claimed delivery to every recipient it has not reached yet.
func (s *service) deliver(ctx context.Context, d *models.ClientReportDelivery) {
	token := emailToken(s.LinkKey, d.ID)
	if hash := hashToken(token); !bytes.Equal(hash, d.TokenHash) {
		// The key changed since the run was recorded; a recipient already reached holds the old link.
		if len(d.SentTo) > 0 {
			s.finish(ctx, d, nil, "the link key changed after part of this run was sent")
			return
		}
		if err := s.Repo.SetDeliveryToken(ctx, d.ID, hash); err != nil {
			s.finish(ctx, d, nil, "could not store the run's link")
			return
		}
	}
	rep, err := s.Repo.Get(ctx, d.OrganizationID, d.ReportID)
	if err != nil || rep == nil {
		s.finish(ctx, d, nil, "the report could not be read")
		return
	}
	url := config.ReportURLOn(s.linkHost(ctx, d.OrganizationID), token)
	if url == "" {
		s.finish(ctx, d, nil, "no public page host is configured")
		return
	}
	view, xerr := s.render(ctx, rep, d.DateRange)
	if xerr != nil {
		s.finish(ctx, d, nil, "the report's numbers could not be read")
		return
	}
	brand, err := s.Brands.ForOrganization(ctx, d.OrganizationID)
	if err != nil {
		brand = brandkit.Default()
	}
	subject, html, err := reportEmail(rep, view, brand, url)
	if err != nil {
		s.finish(ctx, d, nil, "the email could not be rendered")
		return
	}
	for _, addr := range d.Recipients {
		if slices.Contains(d.SentTo, addr) {
			continue
		}
		if err := s.Mail.Send(ctx, []string{addr}, nil, nil, subject, html); err != nil {
			log.Warn().Err(err).Str("report_id", d.ReportID.String()).Msg("client reports: sending a scheduled report failed")
			s.finish(ctx, d, nil, "the mail transport refused the message")
			return
		}
		if err := s.Repo.MarkDeliveredTo(ctx, d.ID, addr); err != nil {
			log.Warn().Err(err).Str("delivery_id", d.ID.String()).Msg("client reports: recording a recipient failed")
		}
	}
	now := s.now()
	s.finish(ctx, d, &now, "")
}

func (s *service) finish(ctx context.Context, d *models.ClientReportDelivery, sentAt *time.Time, reason string) {
	if err := s.Repo.FinishDelivery(ctx, d.ID, sentAt, reason); err != nil {
		log.Warn().Err(err).Str("delivery_id", d.ID.String()).Msg("client reports: finishing a delivery failed")
	}
}

// reportEmail renders the summary email; every name in it is display-safe or replaced.
func reportEmail(rep *models.ClientReport, v *models.ClientReportView, brand brandkit.Brand, url string) (string, string, error) {
	brandName := displayname.DisplayableOr(brand.Name, "Your team")
	reportName := displayname.DisplayableOr(rep.Name, "Your report")
	period := periodLabel(v)
	in := templates.ClientReportEmail{
		BrandName: brandName, LogoURL: brand.LogoURL, AccentColor: brand.AccentColor,
		ReportName: reportName, Period: period, ReportURL: url,
	}
	if brand.PoweredBy != nil {
		in.PoweredByName = displayname.Displayable(brand.PoweredBy.Name)
		in.PoweredByURL = brand.PoweredBy.URL
	}
	if o := v.Overview; o != nil {
		in.Metrics = append(in.Metrics,
			templates.ClientReportMetric{Label: "Sent", Value: count(o.Sent)},
			templates.ClientReportMetric{Label: "Replies", Value: count(o.Replied), Note: percent(o.ReplyRate) + " reply rate"},
		)
		if o.Positive != nil {
			in.Metrics = append(in.Metrics, templates.ClientReportMetric{Label: "Positive", Value: count(*o.Positive)})
		}
		in.Metrics = append(in.Metrics, templates.ClientReportMetric{Label: "Bounce rate", Value: percent(o.BounceRate)})
	}
	for i, c := range v.Campaigns {
		if i == emailCampaignRows {
			break
		}
		in.Campaigns = append(in.Campaigns, templates.ClientReportEmailCampaign{
			Name: displayname.DisplayableOr(c.Name, "A campaign"), Sent: count(c.Sent), Replies: count(c.Replies)})
	}
	html, err := templates.GenerateClientReportHTML(in)
	return reportName + ": " + period, html, err
}

func periodLabel(v *models.ClientReportView) string {
	from, err1 := time.Parse(dayLayout, v.From)
	to, err2 := time.Parse(dayLayout, v.To)
	if err1 != nil || err2 != nil {
		return ""
	}
	if from.Equal(to) {
		return to.Format("Jan 2, 2006")
	}
	if from.Year() == to.Year() {
		return from.Format("Jan 2") + " to " + to.Format("Jan 2, 2006")
	}
	return from.Format("Jan 2, 2006") + " to " + to.Format("Jan 2, 2006")
}

func count(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 && n > -1000 {
		return s
	}
	neg := n < 0
	if neg {
		s = s[1:]
	}
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func percent(v float64) string {
	return fmt.Sprintf("%.1f%%", v)
}
