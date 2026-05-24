# Frontend Development Guide

How to run the backend locally with Docker Compose while developing the frontend.

## Do I need a .env file?

No. All environment variables are already defined inline in `deploy/docker/docker-compose.yml` with working local defaults. You don't need to create or manage any `.env` file to get started.

If you need to override something (e.g. real Google OAuth credentials for testing), edit the environment variables directly in the `docker-compose.yml` under the relevant service.

## Starting the backend

```bash
cd deploy/docker

# Start everything
docker-compose up -d
```

This gives you:

| Service | URL |
|---------|-----|
| Backend API | http://localhost:8080 |
| Realtime (WebSocket) | ws://localhost:4000/socket |
| Tracking | http://localhost:3000 |
| Mailpit (email inbox) | http://localhost:8025 |

### Lighter setup (API only)

If you don't need realtime or tracking:

```bash
cd deploy/docker
docker-compose up -d postgres redis kafka zookeeper schema-registry backend
```

Add more services as needed:

```bash
docker-compose up -d realtime    # WebSocket support
docker-compose up -d tracking    # Open/click tracking
docker-compose up -d consumer    # Event processing
```

## Frontend config

Point your frontend at the local services:

```env
API_URL=http://localhost:8080
WS_URL=ws://localhost:4000/socket
TRACKING_URL=http://localhost:3000
```

CORS is set to allow all origins in dev mode, so any frontend port works.

## Common commands

```bash
cd deploy/docker

# Rebuild after pulling new backend code
docker-compose up -d --build backend

# View logs
docker-compose logs -f backend

# Reset database (wipes all data)
docker-compose down -v && docker-compose up -d

# Stop everything
docker-compose down
```

## Email in local dev

All emails sent by the backend (login codes, verification codes, password resets) are captured by **Mailpit**. Open http://localhost:8025 to view them — no real email is ever sent in local dev.

## Which services do I need?

| What you're building | Run |
|---------------------|-----|
| Most UI work (auth, campaigns, contacts, settings) | infra + `backend` + `mailpit` |
| Live updates / realtime UI | above + `realtime` |
| Email tracking / analytics | above + `tracking consumer` |
| Full integration | `docker-compose up -d` |
