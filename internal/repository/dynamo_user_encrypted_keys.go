package repository

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
)

type UserEncryptedKeysRepository interface {
	Put(ctx context.Context, data UserEncryptedKeysItem) error
	Get(ctx context.Context, userID uuid.UUID) (string, error)
	Del(ctx context.Context, userID uuid.UUID) error
}

type userEncryptedKeysRepository struct {
	kms *kms.KMS
	db  *dynamo.Client
}

func NewUserEncryptedKeysRepository(
	kms *kms.KMS,
	db *dynamo.Client,
) UserEncryptedKeysRepository {
	return &userEncryptedKeysRepository{
		kms,
		db,
	}
}

type UserEncryptedKeysItem struct {
	UserID           string `dynamodbav:"userId"`
	EncryptedDataKey string `dynamodbav:"encryptedDataKey"`
}

const UserEncryptedKeysTable = "UserEncryptedKeys"

func (r *userEncryptedKeysRepository) Put(ctx context.Context, data UserEncryptedKeysItem) error {
	body, err := attributevalue.MarshalMap(data)
	if err != nil {
		return err
	}

	_, err = r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(UserEncryptedKeysTable),
		Item:                body,
		ConditionExpression: aws.String("attribute_not_exists(userId)"),
	})
	if err != nil {
		return err
	}

	return nil
}

func (r *userEncryptedKeysRepository) Get(ctx context.Context, userID uuid.UUID) (string, error) {
	resp, err := r.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(EmailMessageMapTable),
		Key: map[string]types.AttributeValue{
			"userId": &types.AttributeValueMemberS{Value: userID.String()},
		},
	})
	if err != nil {
		return "", err
	}

	if len(resp.Item) == 0 {
		return "", nil
	}

	var item UserEncryptedKeysItem
	if err := attributevalue.UnmarshalMap(resp.Item, &item); err != nil {
		return "", err
	}

	return item.EncryptedDataKey, nil
}

func (r *userEncryptedKeysRepository) Del(ctx context.Context, userID uuid.UUID) error {
	var keys map[string]types.AttributeValue = map[string]types.AttributeValue{
		"userId": &types.AttributeValueMemberS{Value: userID.String()},
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
