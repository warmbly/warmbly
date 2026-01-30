package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
)

type EmailHistoryIDRepository interface {
	Put(ctx context.Context, userID, emailID uuid.UUID, historyID uint64) error
	Get(ctx context.Context, userID, emailID uuid.UUID) (*EmailHistoryIDData, error)
}

type emailHistoryIDRepository struct {
	db *dynamo.Client
}

func NewEmailHistoryIDRepository(db *dynamo.Client) EmailHistoryIDRepository {
	return &emailHistoryIDRepository{
		db: db,
	}
}

const EmailHistoryIDTable = "EmailHistoryID"

type EmailHistoryIDData struct {
	UserID        string    `dynamodbav:"userId"`
	EmailID       string    `dynamodbav:"emailId"`
	HistoryID     string    `dynamodbav:"historyId"`
	LastUpdatedAt time.Time `dynamodbav:"lastUpdatedAt"`
}

// Put inserts or updates the historyID for a user/email
func (r *emailHistoryIDRepository) Put(ctx context.Context, userID, emailID uuid.UUID, historyID uint64) error {
	item := map[string]types.AttributeValue{
		"userId":        &types.AttributeValueMemberS{Value: userID.String()},
		"emailId":       &types.AttributeValueMemberS{Value: emailID.String()},
		"historyId":     &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", historyID)},
		"lastUpdatedAt": &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)},
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(EmailHistoryIDTable),
		Item:      item,
	}

	_, err := r.db.PutItem(ctx, input)
	return err
}

// Get retrieves the historyID data for a user/email
func (r *emailHistoryIDRepository) Get(ctx context.Context, userID, emailID uuid.UUID) (*EmailHistoryIDData, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(EmailHistoryIDTable),
		Key: map[string]types.AttributeValue{
			"userId":  &types.AttributeValueMemberS{Value: userID.String()},
			"emailId": &types.AttributeValueMemberS{Value: emailID.String()},
		},
	}

	resp, err := r.db.GetItem(ctx, input)
	if err != nil {
		return nil, err
	}

	if len(resp.Item) == 0 {
		return nil, nil
	}

	var data EmailHistoryIDData
	if err := attributevalue.UnmarshalMap(resp.Item, &data); err != nil {
		return nil, err
	}

	return &data, nil
}
