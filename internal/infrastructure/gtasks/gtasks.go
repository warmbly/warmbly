package gtasks

import (
	"context"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"github.com/warmbly/warmbly/internal/tasks/proto"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Client struct {
	serviceAccountEmail string
	url                 string
	queueName           string
	client              *cloudtasks.Client
}

func NewClient(ctx context.Context, queueName, url, serviceAccountEmail, emulatorHost string) (*Client, error) {
	var opts []option.ClientOption
	if emulatorHost != "" {
		opts = append(opts,
			option.WithEndpoint(emulatorHost),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
	}

	c, err := cloudtasks.NewClient(ctx, opts...)
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

func (c *Client) CreateTask(ctx context.Context, taskData *proto.ProcessTask, scheduleTime time.Time) (string, error) {
	body, err := gproto.Marshal(taskData)
	if err != nil {
		return "", err
	}

	task := &cloudtaskspb.Task{
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
					"Content-Type": "application/octet-stream",
				},
			},
		},
	}

	// Add schedule time if provided and in the future
	if !scheduleTime.IsZero() && scheduleTime.After(time.Now()) {
		task.ScheduleTime = timestamppb.New(scheduleTime)
	}

	req := &cloudtaskspb.CreateTaskRequest{
		Parent: c.queueName,
		Task:   task,
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
