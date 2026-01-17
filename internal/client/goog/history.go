package goog

import "context"

func (c *Client) FetchHistory(ctx context.Context, lastHistoryID uint64) (uint64, error) {
	call := c.srv.Users.History.List("me").MaxResults(500).StartHistoryId(lastHistoryID) // It does not include the record that has that exact HistoryID

	var newLastHistoryID uint64

	for {
		resp, err := call.Do()
		if err != nil {
			return newLastHistoryID, HandleError(err)
		}

		for _, h := range resp.History {
			for _, m := range h.MessagesAdded {
				msg := GmailMessageToEmailData(m.Message)
				if err := c.OnMessageAdd(ctx, msg); err != nil {
					return newLastHistoryID, err
				}
			}
			for _, m := range h.MessagesDeleted {
				if err := c.OnMessageRemove(ctx, m.Message.Id); err != nil {
					return newLastHistoryID, err
				}
			}
			for _, m := range h.LabelsAdded {
				if err := c.OnLabelAdd(ctx, m.Message.Id, m.LabelIds); err != nil {
					return newLastHistoryID, err
				}
			}
			for _, m := range h.LabelsRemoved {
				if err := c.OnLabelRemove(ctx, m.Message.Id, m.LabelIds); err != nil {
					return newLastHistoryID, err
				}
			}
		}
		newLastHistoryID = resp.HistoryId
		if resp.NextPageToken == "" {
			break
		}
		call.PageToken(resp.NextPageToken)
	}

	return newLastHistoryID, nil
}
