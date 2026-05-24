import Config

# Test configuration
config :realtime, RealtimeWeb.Endpoint,
  http: [ip: {127, 0, 0, 1}, port: 4002],
  secret_key_base: "test_secret_key_base_for_testing_purposes_only_1234567890123456",
  server: false

config :realtime,
  jwt_secret: "test_jwt_secret",
  pubsub_enabled: false

config :logger, level: :warning

config :sentry,
  dsn: nil
