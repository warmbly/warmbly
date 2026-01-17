package unibox

import (
	"bytes"
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
)

func GetEmailKey(userID, id uuid.UUID) string {
	return "emails/" + userID.String() + "/" + id.String()

}

func (s *uniboxService) GetBody(
	ctx context.Context,
	userID, id uuid.UUID,
) (*emsg.EmailBlob, error) {
	key := GetEmailKey(userID, id)
	object, err := s.s3.GetObject(
		ctx,
		&s3.GetObjectInput{
			Bucket: &s.s3.Bucket,
			Key:    &key,
		},
	)
	if err != nil {
		return nil, err
	}

	obj, err := emsg.DecodeBinary(object.Body)

	return obj, nil
}

func (s *uniboxService) PutBody(
	ctx context.Context,
	userID, id uuid.UUID,
	plainText string,
	htmlText string,
) error {
	key := GetEmailKey(userID, id)

	blob := &emsg.EmailBlob{
		PlainText: []byte(plainText),
		HTMLBody:  []byte(htmlText),
	}

	body, err := blob.EncodeBinary()
	if err != nil {
		return err
	}

	if _, err := s.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.s3.Bucket,
		Key:    &key,
		Body:   bytes.NewReader(body),
	}); err != nil {
		return err
	}

	return nil
}
