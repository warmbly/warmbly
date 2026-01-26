import Config

config :warmbly_ws,
  port: String.to_integer(System.get_env("PORT") || "4000"),
  redis_url: System.get_env("REDIS_URL") || "redis://localhost:6379"

import_config "config.secret.exs"
