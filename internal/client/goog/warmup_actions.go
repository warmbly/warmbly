package goog

import (
	"context"
	"fmt"

	"google.golang.org/api/gmail/v1"
)

// ApplyLabel applies a label to a message, creating the label if it doesn't exist
func (c *Client) ApplyLabel(ctx context.Context, messageID, labelName string) error {
	if c.srv == nil {
		return fmt.Errorf("gmail service not initialized")
	}

	// Find or create label
	labelID, err := c.getOrCreateLabel(ctx, labelName)
	if err != nil {
		return fmt.Errorf("failed to get/create label %q: %w", labelName, err)
	}

	_, err = c.srv.Users.Messages.Modify("me", messageID, &gmail.ModifyMessageRequest{
		AddLabelIds: []string{labelID},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to apply label: %w", err)
	}

	return nil
}

// MarkAsRead marks a message as read by removing the UNREAD label
func (c *Client) MarkAsRead(ctx context.Context, messageID string) error {
	if c.srv == nil {
		return fmt.Errorf("gmail service not initialized")
	}

	_, err := c.srv.Users.Messages.Modify("me", messageID, &gmail.ModifyMessageRequest{
		RemoveLabelIds: []string{Unread},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to mark as read: %w", err)
	}

	return nil
}

// RemoveFromSpam removes a message from the SPAM label
func (c *Client) RemoveFromSpam(ctx context.Context, messageID string) error {
	if c.srv == nil {
		return fmt.Errorf("gmail service not initialized")
	}

	_, err := c.srv.Users.Messages.Modify("me", messageID, &gmail.ModifyMessageRequest{
		RemoveLabelIds: []string{Spam},
		AddLabelIds:    []string{Inbox},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to remove from spam: %w", err)
	}

	return nil
}

// MarkImportant marks a message as important
func (c *Client) MarkImportant(ctx context.Context, messageID string) error {
	if c.srv == nil {
		return fmt.Errorf("gmail service not initialized")
	}

	_, err := c.srv.Users.Messages.Modify("me", messageID, &gmail.ModifyMessageRequest{
		AddLabelIds: []string{Important},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to mark important: %w", err)
	}

	return nil
}

// getOrCreateLabel finds an existing label or creates a new one
func (c *Client) getOrCreateLabel(ctx context.Context, labelName string) (string, error) {
	// List existing labels
	resp, err := c.srv.Users.Labels.List("me").Context(ctx).Do()
	if err != nil {
		return "", err
	}

	for _, label := range resp.Labels {
		if label.Name == labelName {
			return label.Id, nil
		}
	}

	// Create new label
	newLabel, err := c.srv.Users.Labels.Create("me", &gmail.Label{
		Name:                  labelName,
		LabelListVisibility:   "labelShow",
		MessageListVisibility: "show",
	}).Context(ctx).Do()
	if err != nil {
		return "", err
	}

	return newLabel.Id, nil
}
