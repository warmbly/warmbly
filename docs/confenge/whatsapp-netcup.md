# Netcup ops: Evolution API for CONFENGE WhatsApp

## Compose project

```bash
cd deploy/evolution
cp .env.example .env
# fill EVOLUTION_API_KEY, EVOLUTION_DB_PASSWORD
docker compose -p evolution-confenge --env-file .env up -d
docker compose -p evolution-confenge ps
```

- Image pin: `evoapicloud/evolution-api:2.3.7`
- Own Postgres + Redis volumes (`evolution_pg_data`, `evolution_redis_data`)
- Bound to `127.0.0.1:8089` by default
- Do not share Warmbly/extra-cli databases

## Warmbly env (private network)

```env
CONFENGE_WHATSAPP_ENABLED=false
WHATSAPP_PROVIDER=evolution
WHATSAPP_EVOLUTION_BASE_URL=http://127.0.0.1:8089
WHATSAPP_EVOLUTION_API_KEY=...
WHATSAPP_EVOLUTION_INSTANCE=confenge
WHATSAPP_EVOLUTION_ALLOW_BAILEYS=false
WHATSAPP_WEBHOOK_SECRET=...
CONFENGE_CROSS_CHANNEL_MIN_INTERVAL_HOURS=24
```

Webhook: Evolution → `https://<warmbly-host>/api/v1/webhooks/evolution/<instance>` with Bearer secret.

## Manual Meta steps (external)

1. Meta Business verification / WABA
2. Connect Cloud API number on Evolution (`WHATSAPP-BUSINESS`)
3. Approve official templates
4. Enable `CONFENGE_WHATSAPP_ENABLED` only after smoke on operator-controlled numbers

## Rollback

1. Set `CONFENGE_WHATSAPP_ENABLED=false` (and auto-send/auto-reply off)
2. `docker compose -p evolution-confenge stop`
3. Keep volumes for backup; do not drop unless intentional

## License notice

Evolution API is used as a WhatsApp transport gateway. See `evolution-api-compliance.md`.
