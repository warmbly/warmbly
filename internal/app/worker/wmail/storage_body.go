package wmail

import (
	"bytes"
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
)

func (c *WMail) Exists(ctx context.Context, messageID uuid.UUID) (bool, error) {
	key := config.StorageEndpointEmailBody(c.UserID, c.ID, messageID)
	val, err := c.Storage.Exists(ctx, c.Storage.Bucket, key)
	if err != nil {
		return false, err
	}

	return val, nil
}

func (c *WMail) StoreBody(ctx context.Context, emailMessageID uuid.UUID, data *emsg.EmailBlob) error {
	bytedata, err := data.EncodeBinary()
	if err != nil {
		return err
	}

	key := config.StorageEndpointEmailBody(c.UserID, c.ID, emailMessageID)

	_, err = c.Storage.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &c.Storage.Bucket,
		Key:    &key,
		Body:   bytes.NewReader(bytedata),
	})
	if err != nil {
		return err
	}

	return nil
}
