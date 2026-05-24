import Config

# Production configuration
config :realtime, RealtimeWeb.Endpoint,
  cache_static_manifest: false,
  server: true

# Production logger - structured format
config :logger, :console,
  format: "$time $metadata[$level] $message\n",
  metadata: [:request_id, :user_id, :remote_ip]

config :logger,
  level: :info
