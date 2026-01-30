import Config

# Production configuration
config :realtime, RealtimeWeb.Endpoint,
  cache_static_manifest: false,
  server: true

# Production logger - JSON format for structured logging
config :logger, :console,
  format: {Jason, :encode!},
  metadata: [:request_id, :user_id, :remote_ip]

config :logger,
  level: :info
