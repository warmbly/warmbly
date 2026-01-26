import Config

if config_env() == :prod do
  config :my_app,
    port: String.to_integer(System.get_env("PORT") || "4000"),
    redis_url: System.fetch_env!("REDIS_URL"),
    discord_webhook_url: System.fetch_env!("DISCORD_WEBHOOK")

end
