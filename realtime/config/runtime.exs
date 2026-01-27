import Config

if config_env() == :prod do
  # Required environment variables
  jwt_secret =
    System.get_env("JWT_SECRET") ||
      raise "JWT_SECRET environment variable is required"

  secret_key_base =
    System.get_env("SECRET_KEY_BASE") ||
      raise "SECRET_KEY_BASE environment variable is required"

  gcp_project_id =
    System.get_env("GCP_PROJECT_ID") ||
      raise "GCP_PROJECT_ID environment variable is required"

  database_url =
    System.get_env("DATABASE_URL") ||
      raise "DATABASE_URL environment variable is required"

  port = String.to_integer(System.get_env("PORT") || "4000")
  host = System.get_env("PHX_HOST") || "localhost"

  config :realtime,
    port: port,
    jwt_secret: jwt_secret,
    gcp_project_id: gcp_project_id,
    pubsub_enabled: System.get_env("PUBSUB_ENABLED") == "true",
    pubsub_subscriptions: [
      "task-status-sub",
      "campaign-update-sub",
      "warmup-update-sub",
      "email-error-sub",
      "email-warning-sub",
      "user-events-sub",
      "email-inbox-sub",
      "bulk-operations-sub",
      "contacts-sync-sub"
    ],
    # Redis configuration
    redis_url: System.get_env("REDIS_URL") || "redis://localhost:6379/0",
    # Connection limits
    max_connections_per_user: String.to_integer(System.get_env("MAX_CONNECTIONS_PER_USER") || "10"),
    max_connections_per_ip: String.to_integer(System.get_env("MAX_CONNECTIONS_PER_IP") || "50"),
    max_connections_global: String.to_integer(System.get_env("MAX_CONNECTIONS_GLOBAL") || "100000"),
    # Rate limits (per minute)
    rate_limit_ws_message: String.to_integer(System.get_env("RATE_LIMIT_WS_MESSAGE") || "120"),
    rate_limit_ws_join: String.to_integer(System.get_env("RATE_LIMIT_WS_JOIN") || "30"),
    rate_limit_ws_event: String.to_integer(System.get_env("RATE_LIMIT_WS_EVENT") || "60")

  config :realtime, RealtimeWeb.Endpoint,
    url: [host: host, port: 443, scheme: "https"],
    http: [
      ip: {0, 0, 0, 0, 0, 0, 0, 0},
      port: port
    ],
    secret_key_base: secret_key_base,
    check_origin: System.get_env("CHECK_ORIGIN", "false") == "true"

  # Ecto Repo configuration (for API key validation)
  config :realtime, Realtime.Repo,
    url: database_url,
    pool_size: String.to_integer(System.get_env("DATABASE_POOL_SIZE") || "10")

  # Sentry configuration
  if sentry_dsn = System.get_env("SENTRY_DSN") do
    config :sentry,
      dsn: sentry_dsn,
      environment_name: :prod,
      included_environments: [:prod]
  end

  # Goth for GCP authentication
  if gcp_credentials = System.get_env("GOOGLE_APPLICATION_CREDENTIALS_JSON") do
    config :goth, json: gcp_credentials
  end
end
