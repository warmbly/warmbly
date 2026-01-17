package notify

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/getsentry/sentry-go"
)

type EmailNotificationService interface {
	Send(ctx context.Context, to, cc, bcc []string, subject, message string) error
}

type emailNotificationService struct {
	Name    string
	Address string
	Client  *sesv2.Client
	From    string
}

func NewEmailNotficiationService(ctx context.Context, name, address string) (EmailNotificationService, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}

	client := sesv2.NewFromConfig(cfg)
	return &emailNotificationService{
		Name:    name,
		Address: address,
		Client:  client,
	}, nil
}

func (s *emailNotificationService) Send(ctx context.Context, to, cc, bcc []string, subject, message string) error {
	from := fmt.Sprintf("%s <%s>", s.Name, s.From)

	input := &sesv2.SendEmailInput{
		Destination: &types.Destination{
			ToAddresses:  to,
			CcAddresses:  cc,
			BccAddresses: bcc,
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Body: &types.Body{
					Html: &types.Content{
						Data: &message,
					},
				},
				Subject: &types.Content{
					Data: &subject,
				},
			},
		},
		FromEmailAddress: &from,
	}

	_, err := s.Client.SendEmail(ctx, input)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}

	return err
}
