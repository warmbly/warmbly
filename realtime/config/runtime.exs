import Config

if config_env() == :prod do
  # Required environment variables
  jwt_secret =
    System.get_env("JWT_SECRET") ||
      raise "JWT_SECRET environment variable is required"

  secret_key_base =
    System.get_env("SECRET_KEY_BASE") ||
      raise "SECRET_KEY_BASE environment variable is required"

  pubsub_enabled = System.get_env("PUBSUB_ENABLED") == "true"

  # Only the Pub/Sub subscriber needs a GCP project; the no-cloud stack runs
  # with PUBSUB_ENABLED=false and no GCP account.
  gcp_project_id =
    System.get_env("GCP_PROJECT_ID") ||
      if pubsub_enabled do
        raise "GCP_PROJECT_ID environment variable is required when PUBSUB_ENABLED=true"
      else
        ""
      end

  database_url =
    System.get_env("DATABASE_URL") ||
      raise "DATABASE_URL environment variable is required"

  port = String.to_integer(System.get_env("PORT") || "4000")
  host = System.get_env("PHX_HOST") || "localhost"

  config :realtime,
    port: port,
    jwt_secret: jwt_secret,
    gcp_project_id: gcp_project_id,
    pubsub_enabled: pubsub_enabled,
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
    max_connections_per_user:
      String.to_integer(System.get_env("MAX_CONNECTIONS_PER_USER") || "10"),
    max_connections_per_ip: String.to_integer(System.get_env("MAX_CONNECTIONS_PER_IP") || "50"),
    max_connections_global:
      String.to_integer(System.get_env("MAX_CONNECTIONS_GLOBAL") || "100000"),
    # Rate limits (per minute)
    rate_limit_ws_message: String.to_integer(System.get_env("RATE_LIMIT_WS_MESSAGE") || "120"),
    rate_limit_ws_connect: String.to_integer(System.get_env("RATE_LIMIT_WS_CONNECT") || "30"),
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

  # Postgrex verifies the server against the system CA store, which has no
  # Amazon RDS root in it, so an RDS database needs DATABASE_SSL_CA_FILE
  # pointing at a bundle. The image ships AWS's at
  # /etc/ssl/rds/global-bundle.pem. It is opt-in rather than the default for
  # the same reason the backend makes sslrootcert opt-in: pointing every
  # install at an RDS-only store would break a Postgres fronted by a public CA.
  database_ssl =
    cond do
      System.get_env("DATABASE_SSL", "true") != "true" ->
        false

      ca_file = System.get_env("DATABASE_SSL_CA_FILE") ->
        [
          verify: :verify_peer,
          cacertfile: to_charlist(ca_file),
          customize_hostname_check: [
            match_fun: :public_key.pkix_verify_hostname_match_fun(:https)
          ]
        ]

      true ->
        true
    end

  # Ecto Repo configuration (for API key validation)
  config :realtime, Realtime.Repo,
    url: database_url,
    ssl: database_ssl,
    pool_size: String.to_integer(System.get_env("DATABASE_POOL_SIZE") || "10"),
    show_sensitive_data_on_connection_error: true

  # Error reporting. An env var that is present but empty must behave as unset:
  # compose passes every optional variable through as "" so a single .env can
  # drive the whole stack, and Sentry rejects "" as an invalid DSN hard enough
  # to take the whole node down at boot.
  # A variable that is present but blank counts as unset here too, for the same
  # reason the DSN does: compose passes every optional variable through as "",
  # and System.get_env/2 only applies its default when the name is absent.
  env_or = fn name, fallback ->
    case System.get_env(name) do
      value when is_binary(value) ->
        case String.trim(value) do
          "" -> fallback
          trimmed -> trimmed
        end

      _ ->
        fallback
    end
  end

  # PostHog error tracking, the default backend. The key also carries product
  # analytics elsewhere in the platform, so POSTHOG_ERROR_TRACKING=false keeps
  # the key configured and reports nothing from here.
  config :realtime,
    posthog_key:
      if(env_or.("POSTHOG_ERROR_TRACKING", "true") != "false",
        do: env_or.("POSTHOG_KEY", nil),
        else: nil
      ),
    posthog_host: env_or.("POSTHOG_HOST", nil)

  case System.get_env("SENTRY_DSN") do
    dsn when is_binary(dsn) and dsn != "" ->
      # release ties a stack trace to a build, the same value every other
      # service tags with. The image sets it from the release tag; an
      # unstamped build reports "dev".
      config :sentry,
        dsn: dsn,
        environment_name: env_or.("APP_ENV", "prod"),
        release: env_or.("WARMBLY_RELEASE", "dev")

    _ ->
      :ok
  end

  # Goth for GCP authentication
  case System.get_env("GOOGLE_APPLICATION_CREDENTIALS_JSON") do
    creds when is_binary(creds) and creds != "" ->
      config :goth, json: creds

    _ ->
      :ok
  end
end
