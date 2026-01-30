defmodule Realtime.Application do
  @moduledoc false

  use Application

  require Logger

  @impl true
  def start(_type, _args) do
    children = [
      # Database repository for API key lookups
      Realtime.Repo,

      # Redis connection pool for rate limiting and distributed state
      Realtime.Redis,

      # Phoenix PubSub for internal message broadcasting
      {Phoenix.PubSub, name: Realtime.PubSub},

      # Phoenix Endpoint (WebSocket server)
      RealtimeWeb.Endpoint,

      # Connection tracker
      {Realtime.Connections, []},

      # Google Pub/Sub subscriber supervisor
      {Realtime.PubSub.Supervisor, []}
    ]

    opts = [strategy: :one_for_one, name: Realtime.Supervisor]

    Logger.info("Starting Realtime application...")
    Logger.info("Redis URL: #{Application.get_env(:realtime, :redis_url, "not configured")}")
    Logger.info("Connection limits: user=#{Application.get_env(:realtime, :max_connections_per_user, 10)}, ip=#{Application.get_env(:realtime, :max_connections_per_ip, 50)}, global=#{Application.get_env(:realtime, :max_connections_global, 100_000)}")
    Logger.info("Rate limits: message=#{Application.get_env(:realtime, :rate_limit_ws_message, 120)}/min, join=#{Application.get_env(:realtime, :rate_limit_ws_join, 30)}/min, event=#{Application.get_env(:realtime, :rate_limit_ws_event, 60)}/min")

    Supervisor.start_link(children, opts)
  end

  @impl true
  def config_change(changed, _new, removed) do
    RealtimeWeb.Endpoint.config_change(changed, removed)
    :ok
  end
end
