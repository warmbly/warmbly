# Event System

Warmbly uses Apache Kafka for asynchronous event streaming between services, with Avro serialization and Confluent Schema Registry.

## Overview

Two producers write to Kafka and two consumers read from it: the tracking service publishes events that the consumer processes, and the backend publishes commands that the workers execute.

## Kafka Topics

| Topic | Producer | Consumer | Description |
|-------|----------|----------|-------------|
| `tracking-events` | Tracking | Consumer | Email opens, link clicks |
| `worker:{id}` | Backend | Worker | Commands to specific worker |
| `jobs:worker-events` | Worker | Consumer | Job completion events |
| `email-events` | Worker | Consumer | Email sync, send results |
| `campaign-events` | Backend/Worker | Consumer | Campaign progress updates |

## Event Types

### Tracking Events

Produced by the Rust tracking service when tracking pixels are loaded or links are clicked.

```json
{
  "type": "OPEN" | "CLICK",
  "tracking_id": "uuid",
  "timestamp": "2026-01-29T12:00:00Z",
  "ip": "1.2.3.4",
  "user_agent": "Mozilla/5.0...",
  "link_url": "https://..."  // only for CLICK
}
```

### Worker Events

Commands sent to workers for email operations.

```json
{
  "type": "SEND_EMAIL" | "SYNC_INBOX" | "ADD_ACCOUNT",
  "worker_id": "w1",
  "account_id": "uuid",
  "payload": { ... }
}
```

### Job Events

Responses from workers after completing operations.

```json
{
  "type": "EMAIL_SENT" | "SYNC_COMPLETED" | "ERROR",
  "job_id": "uuid",
  "worker_id": "w1",
  "result": { ... },
  "error": null
}
```

### Campaign Events

Progress updates for email campaigns.

```json
{
  "type": "CAMPAIGN_PROGRESS",
  "campaign_id": "uuid",
  "sent": 150,
  "opened": 45,
  "clicked": 12,
  "bounced": 2
}
```

## Avro Serialization

Events are serialized using Avro with schemas registered in Confluent Schema Registry.

### Schema Registry

- Local: http://localhost:8081
- Production: Confluent Cloud

### Example Schema (TrackingEvent)

```json
{
  "type": "record",
  "name": "TrackingEvent",
  "namespace": "com.warmbly.events",
  "fields": [
    {"name": "type", "type": "string"},
    {"name": "tracking_id", "type": "string"},
    {"name": "timestamp", "type": "long", "logicalType": "timestamp-millis"},
    {"name": "ip", "type": ["null", "string"], "default": null},
    {"name": "user_agent", "type": ["null", "string"], "default": null},
    {"name": "link_url", "type": ["null", "string"], "default": null}
  ]
}
```

## Configuration

### Environment Variables

```bash
# Kafka
KAFKA_BOOTSTRAP_SERVERS=localhost:9092
KAFKA_CONSUMER_GROUP=consumer-group
KAFKA_TRACKING_TOPIC=tracking-events

# Schema Registry
SCHEMA_REGISTRY_URL=http://localhost:8081

# For Confluent Cloud (production)
KAFKA_SASL_USERNAME=your-api-key
KAFKA_SASL_PASSWORD=your-api-secret
SCHEMA_REGISTRY_KEY=your-sr-key
SCHEMA_REGISTRY_SECRET=your-sr-secret
```

## Code References

- Event schemas: `internal/events/schemas.go`
- Kafka config: `internal/config/config_kafka.go`
- Tracking event consumer: `internal/app/consumer/event_tracking.go`
