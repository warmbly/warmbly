package gtasks

import (
	"context"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"github.com/warmbly/warmbly/internal/tasks/proto"
	gproto "google.golang.org/protobuf/proto"
)

type Client struct {
	serviceAccountEmail string
	url                 string
	queueName           string
	client              *cloudtasks.Client
}

func NewClient(ctx context.Context, queueName, url, serviceAccountEmail string) (*Client, error) {
	c, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return &Client{
		queueName:           queueName,
		client:              c,
		url:                 url,
		serviceAccountEmail: serviceAccountEmail,
	}, nil
}

func (c *Client) CreateTask(ctx context.Context, taskData *proto.CampaignTask) (string, error) {
	body, err := gproto.Marshal(taskData)
	if err != nil {
		return "", err
	}

	req := &cloudtaskspb.CreateTaskRequest{
		Parent: c.queueName,
		Task: &cloudtaskspb.Task{
			MessageType: &cloudtaskspb.Task_HttpRequest{
				HttpRequest: &cloudtaskspb.HttpRequest{
					HttpMethod: cloudtaskspb.HttpMethod_POST,
					Url:        c.url,
					Body:       body,
					AuthorizationHeader: &cloudtaskspb.HttpRequest_OidcToken{
						OidcToken: &cloudtaskspb.OidcToken{
							ServiceAccountEmail: c.serviceAccountEmail,
						},
					},
					Headers: map[string]string{
						"Content-Type": "application/octet-stream", // important!
					},
				},
			},
		},
	}

	out, err := c.client.CreateTask(ctx, req)
	if err != nil {
		return "", err
	}

	return out.Name, nil
}

func (c *Client) DeleteTask(ctx context.Context, name string) error {
	if err := c.client.DeleteTask(ctx, &cloudtaskspb.DeleteTaskRequest{
		Name: name,
	}); err != nil {
		return err
	}

	return nil
}
