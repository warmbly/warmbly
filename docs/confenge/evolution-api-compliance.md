# Evolution API compliance notes (CONFENGE / Warmbly)

## Role

Evolution API is an **external WhatsApp gateway** only.

- Warmbly remains the CRM, policy engine, and execution plane.
- Evolution is not a second CRM. Contacts, pipeline, consent, and commercial state live in Warmbly.
- We do **not** vendor Evolution source into this repository.

## Version pin

| Item | Value |
|------|--------|
| Product | Evolution API |
| Stable pin | **v2.3.7** |
| Docker image | `evoapicloud/evolution-api:2.3.7` |
| Why | Latest stable (non-RC) as of research; 2.4.0-rc series introduces mandatory licensing activation and is pre-release |
| Do not ship | `2.4.0-rc*` for production |

Re-evaluate the pin only after a stable 2.4+ release and a license review.

## License

Evolution API is licensed under **Apache License 2.0** with additional commercial conditions from Evolution Foundation / Evolution API:

1. **Console logo and copyright**: You may not remove or modify logos/copyright in the Evolution API console or frontend components. This does not apply when you do not use those frontend components.
2. **Usage notification**: If Evolution is used as part of a project (including closed-source), display a clear notification that Evolution API is utilized, visible to system administrators and accessible from documentation or settings. Failure may require a commercial license.
3. Contact for commercial licensing: `contato@evolution-api.com`.

Warmbly obligations for CONFENGE ops:

- Keep this document and Netcup runbooks as the admin-visible usage notice.
- Do not strip Evolution console branding if the Manager UI is deployed.
- Do not bypass activation, telemetry, or license servers.
- Do not modify Evolution source to avoid license terms.

## Production integration mode

| Mode | Status |
|------|--------|
| `WHATSAPP-BUSINESS` / Meta Cloud API | **Default production path** |
| `WHATSAPP-BAILEYS` | **Disabled by default**; forbidden when `APP_ENV=production` |

Env:

```env
WHATSAPP_EVOLUTION_ALLOW_BAILEYS=false
```

Baileys (WhatsApp Web multi-device sessions) is **not** a substitute for WhatsApp Business Platform policies. Do not use Baileys to circumvent Meta rules.

## Forbidden practices

Warmbly will not implement:

- Mass cold broadcast
- Number rotation / QR farms
- Anti-ban, fingerprint spoofing, humanization-for-evasion
- Bulk `isOnWhatsApp` enumeration of cold contacts
- Auto-send to public phones without opt-in

## Secrets and network

- Evolution runs in its **own** Compose project with its **own** Postgres volume.
- Do not share Warmbly or extra-cli PostgreSQL with Evolution.
- Prefer private network: internet → reverse proxy → Warmbly webhook; Warmbly → private network → Evolution.
- Do not expose the Evolution Manager publicly without strong auth.

## Telemetry

Document any official telemetry toggles from the Evolution release notes for the pinned version. Do not hack or strip telemetry in violation of license terms. If a supported env var disables non-essential telemetry, choose it consciously and record the choice in the Netcup runbook.

## Related docs

- `docs/confenge/whatsapp-channel.md` — Warmbly channel architecture
- `deploy/evolution/` — isolated Compose project
