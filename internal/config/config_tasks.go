package config

import "context"

type CloudTasksConfig struct {
	QueueName    string
	WebhookURL   string
	EmulatorHost string // empty = use real GCP
}

func (c *Config) LoadCloudTasksConfig(ctx context.Context) (*CloudTasksConfig, error) {
	queueName, err := c.GetString(ctx, "CLOUD_TASKS_QUEUE_NAME", "cloud-tasks/queue-name")
	if err != nil {
		return nil, err
	}

	webhookURL, err := c.GetString(ctx, "CLOUD_TASKS_WEBHOOK_URL", "cloud-tasks/webhook-url")
	if err != nil {
		return nil, err
	}

	emulatorHost := c.GetStringOptional(ctx, "CLOUD_TASKS_EMULATOR_HOST", "cloud-tasks/emulator-host", "")

	return &CloudTasksConfig{
		QueueName:    queueName,
		WebhookURL:   webhookURL,
		EmulatorHost: emulatorHost,
	}, nil
}
