package storage

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (c *Client) Exists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := c.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		var notFound *types.NotFound
		if ok := errors.As(err, &notFound); ok {
			return false, nil // Object doesn't exists
		}
		return false, err
	}

	// Object exists
	return true, nil
}
