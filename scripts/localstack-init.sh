#!/usr/bin/env sh
# LocalStack bootstrap: provisions the AWS resources Warmbly expects in dev.
#
# Idempotent — re-running is safe. Each step:
#   1. KMS key + alias/master-key-dev for envelope encryption
#   2. DynamoDB table UserEncryptedKeys
#   3. DynamoDB table EmailMessageData
#   4. S3 bucket "main"

set -eu

ENDPOINT="${AWS_ENDPOINT_URL:-http://localstack:4566}"
REGION="${AWS_REGION:-us-east-1}"
AWS="aws --endpoint-url=${ENDPOINT} --region=${REGION}"

log() { printf '[localstack-init] %s\n' "$*"; }

# KMS
log "ensuring KMS alias alias/master-key-dev"
if ! $AWS kms describe-key --key-id alias/master-key-dev >/dev/null 2>&1; then
  KEY_ID="$($AWS kms create-key \
    --description "Warmbly dev master key" \
    --key-usage ENCRYPT_DECRYPT \
    --key-spec SYMMETRIC_DEFAULT \
    --query 'KeyMetadata.KeyId' --output text)"
  log "created KMS key $KEY_ID"
  $AWS kms create-alias \
    --alias-name alias/master-key-dev \
    --target-key-id "$KEY_ID"
  log "created alias alias/master-key-dev"
else
  log "alias/master-key-dev already exists"
fi

# DynamoDB: UserEncryptedKeys
log "ensuring DynamoDB table UserEncryptedKeys"
if ! $AWS dynamodb describe-table --table-name UserEncryptedKeys >/dev/null 2>&1; then
  $AWS dynamodb create-table \
    --table-name UserEncryptedKeys \
    --attribute-definitions AttributeName=userId,AttributeType=S \
    --key-schema AttributeName=userId,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST >/dev/null
  log "created UserEncryptedKeys"
else
  log "UserEncryptedKeys already exists"
fi

# DynamoDB: EmailMessageData
log "ensuring DynamoDB table EmailMessageData"
if ! $AWS dynamodb describe-table --table-name EmailMessageData >/dev/null 2>&1; then
  $AWS dynamodb create-table \
    --table-name EmailMessageData \
    --attribute-definitions \
      AttributeName=userId,AttributeType=S \
      AttributeName=messageId,AttributeType=S \
    --key-schema \
      AttributeName=userId,KeyType=HASH \
      AttributeName=messageId,KeyType=RANGE \
    --billing-mode PAY_PER_REQUEST >/dev/null
  log "created EmailMessageData"
else
  log "EmailMessageData already exists"
fi

# S3
log "ensuring S3 bucket main"
if ! $AWS s3api head-bucket --bucket main >/dev/null 2>&1; then
  $AWS s3 mb s3://main >/dev/null
  log "created bucket main"
else
  log "bucket main already exists"
fi

log "done"
