import Config

# Development-specific configuration
config :realtime, RealtimeWeb.Endpoint,
  debug_errors: true,
  code_reloader: false,
  check_origin: false,
  watchers: []

# Development logger
config :logger, :console, format: "[$level] $message\n"

# Disable Sentry in development
config :sentry,
  dsn: nil
