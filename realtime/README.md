# Warmbly Realtime

WebSocket gateway for real-time events in Warmbly. Built with Phoenix Channels and Google Pub/Sub.

## Architecture

The Go backend publishes events to Google Pub/Sub. The Elixir realtime service consumes them and fans each event out to the React frontend over Phoenix Channel WebSockets.

## Channels

- `user:{user_id}` - User-specific events (emails, account status, bulk operations)
- `campaign:{campaign_id}` - Campaign progress and status updates
- `account:{account_id}` - Email account sync status and errors
- `bulk:{operation_id}` - Bulk operation progress

## Event Types

### User Events
- `EMAIL_RECEIVED` - New email in inbox
- `ACCOUNT_CONNECTED` / `ACCOUNT_DISCONNECTED` / `ACCOUNT_ERROR`
- `BULK_STARTED` / `BULK_PROGRESS` / `BULK_COMPLETED`

### Campaign Events
- `CAMPAIGN_STARTED` / `CAMPAIGN_PAUSED` / `CAMPAIGN_COMPLETED`
- `CAMPAIGN_PROGRESS` - Emails sent, opens, clicks

### Account Events
- `ACCOUNT_SYNCED` - Sync completed
- `WARMUP_UPDATE` - Warmup statistics

## Setup

### Prerequisites

- Elixir 1.18+
- Google Cloud project with Pub/Sub enabled

### Environment Variables

```bash
# Required
JWT_SECRET=your_jwt_secret
SECRET_KEY_BASE=your_secret_key_base_min_64_chars
GCP_PROJECT_ID=your_gcp_project

# Optional
PORT=4000
PUBSUB_ENABLED=true
SENTRY_DSN=your_sentry_dsn
GOOGLE_APPLICATION_CREDENTIALS_JSON='{"type":"service_account",...}'
```

### Development

```bash
# Install dependencies
mix deps.get

# Start server
mix phx.server

# Or in interactive mode
iex -S mix phx.server
```

### Production

```bash
# Build release
MIX_ENV=prod mix release

# Run
_build/prod/rel/realtime/bin/realtime start
```

## Client Connection

```javascript
import { Socket } from "phoenix";

const socket = new Socket("wss://realtime.warmbly.com/socket", {
  params: { token: "jwt_token_from_api" }
});

socket.connect();

// Join user channel
const userChannel = socket.channel(`user:${userId}`, {});
userChannel.join()
  .receive("ok", () => console.log("Joined user channel"))
  .receive("error", (resp) => console.error("Unable to join", resp));

// Listen for events
userChannel.on("EMAIL_RECEIVED", (payload) => {
  console.log("New email:", payload);
});

// Join campaign channel
const campaignChannel = socket.channel(`campaign:${campaignId}`, {});
campaignChannel.join();
campaignChannel.on("CAMPAIGN_PROGRESS", (payload) => {
  console.log("Campaign progress:", payload);
});
```

## Endpoints

- `GET /health` - Health check
- `GET /stats` - Connection statistics
- `WS /socket` - WebSocket endpoint
