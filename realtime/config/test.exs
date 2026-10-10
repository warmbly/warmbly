import Config

# Test configuration
config :realtime, RealtimeWeb.Endpoint,
  http: [ip: {127, 0, 0, 1}, port: 4002],
  secret_key_base: "test_secret_key_base_for_testing_purposes_only_1234567890123456",
  server: false

config :realtime,
  jwt_secret: "test_jwt_secret",
  pubsub_enabled: false,
  # Count rate-limit hits in memory: the suite must run with no Redis.
  rate_limit_counter: Realtime.Test.Counter

config :realtime, auth_repo: Realtime.Test.AuthRepo

# No database in the test env: these tests build sockets directly and never
# reach Auth. One connection keeps the unreachable-Postgres retry noise to a
# single line instead of five.
config :realtime, Realtime.Repo, pool_size: 1

config :logger, level: :warning

config :sentry,
  dsn: nil
