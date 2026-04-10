package repository

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
)

type EmailMessageMapRepository interface {
	Add(ctx context.Context, data EmailMessageData) error
	Get(ctx context.Context, userID, emailID uuid.UUID, messageID string) (*EmailMessageData, error)
	Del(ctx context.Context, userID, emailID uuid.UUID, messageID string, id uuid.UUID) error
}

type emailMessageMapRepository struct {
	db *dynamo.Client
}

func NewEmailMessageMapRepository(db *dynamo.Client) EmailMessageMapRepository {
	return &emailMessageMapRepository{
		db: db,
	}
}

const EmailMessageMapTable = "EmailMessageData"

type EmailMessageData struct {
	UserID    string `dynamodbav:"userId"`
	EmailID   string `dynamodbav:"emailId"`
	MessageID string `dynamodbav:"messageId"`
	ID        string `dynamodbav:"id"`
	ThreadID  string `dynamodbav:"threadId"`
}

func (r *emailMessageMapRepository) Add(ctx context.Context, data EmailMessageData) error {
	item, err := attributevalue.MarshalMap(data)
	if err != nil {
		return err
	}

	_, err = r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(EmailMessageMapTable),
		Item:      item,
	})
	if err != nil {
		return err
	}

	return nil
}

func (r *emailMessageMapRepository) Get(ctx context.Context, userID, emailID uuid.UUID, messageID string) (*EmailMessageData, error) {
	resp, err := r.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(EmailMessageMapTable),
		Key: map[string]types.AttributeValue{
			"userId":    &types.AttributeValueMemberS{Value: userID.String()},
			"emailId":   &types.AttributeValueMemberS{Value: emailID.String()},
			"messageId": &types.AttributeValueMemberS{Value: messageID},
		},
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Item) == 0 {
		return nil, nil
	}

	var item EmailMessageData
	if err := attributevalue.UnmarshalMap(resp.Item, &item); err != nil {
		return nil, err
	}

	return &item, nil
}

func (r *emailMessageMapRepository) Del(ctx context.Context, userID, emailID uuid.UUID, messageID string, id uuid.UUID) error {
	var keys map[string]types.AttributeValue = make(map[string]types.AttributeValue)

	keys["userId"] = &types.AttributeValueMemberS{Value: userID.String()}

	if emailID != uuid.Nil {
		keys["emailId"] = &types.AttributeValueMemberS{Value: emailID.String()}
	}

	if messageID != "" {
		keys["messageId"] = &types.AttributeValueMemberS{Value: messageID}
	}

	if id != uuid.Nil {
		keys["id"] = &types.AttributeValueMemberS{Value: id.String()}
	}

	_, err := r.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(EmailMessageMapTable),
		Key:       keys,
	})
	if err != nil {
		return err
	}

	return nil
}
