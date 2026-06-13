defmodule Realtime.Application do
  @moduledoc false

  use Application

  require Logger

  @impl true
  def start(_type, _args) do
    # Ensure postgrex application is fully started before we start the Repo.
    # In releases, dependent apps can sometimes be stopped during restart cycles
    # while named processes (like PubSub.Supervisor) survive, causing conflicts.
    {:ok, _} = Application.ensure_all_started(:postgrex)

    children =
      [
        # Database repository for API key lookups
        Realtime.Repo,

        # Redis connection pool for rate limiting and distributed state
        Realtime.Redis,

        # Phoenix PubSub for internal message broadcasting
        {Phoenix.PubSub, name: Realtime.PubSub},

        # Per-org sequencer pool: assigns the resumable sequence + buffers + and
        # broadcasts org events in order. Must start before the event bridge.
        Realtime.Sequencer,

        # Presence tracker for org-level collaboration (who's online / viewing what)
        RealtimeWeb.Presence,

        # Phoenix Endpoint (WebSocket server)
        RealtimeWeb.Endpoint,

        # Connection tracker
        {Realtime.Connections, []},

        # Google Pub/Sub subscriber supervisor
        {Realtime.CloudPubSub.Supervisor, []}
      ] ++ event_bridge_children()

    opts = [strategy: :one_for_one, name: Realtime.Supervisor]

    Logger.info("Starting Realtime application...")
    Logger.info("Redis URL: #{Application.get_env(:realtime, :redis_url, "not configured")}")

    Logger.info(
      "Connection limits: user=#{Application.get_env(:realtime, :max_connections_per_user, 10)}, ip=#{Application.get_env(:realtime, :max_connections_per_ip, 50)}, global=#{Application.get_env(:realtime, :max_connections_global, 100_000)}"
    )

    Logger.info(
      "Rate limits: message=#{Application.get_env(:realtime, :rate_limit_ws_message, 120)}/min, join=#{Application.get_env(:realtime, :rate_limit_ws_join, 30)}/min, event=#{Application.get_env(:realtime, :rate_limit_ws_event, 60)}/min"
    )

    Supervisor.start_link(children, opts)
  end

  # Bridge backend events over Redis whenever Google Pub/Sub is not the active
  # transport (local dev and any non-GCP env). In Pub/Sub environments the
  # Broadway subscriber handles fan-out, so this stays off and events are never
  # delivered twice.
  defp event_bridge_children do
    if Application.get_env(:realtime, :pubsub_enabled, false) do
      []
    else
      [Realtime.Redis.EventSubscriber]
    end
  end

  @impl true
  def config_change(changed, _new, removed) do
    RealtimeWeb.Endpoint.config_change(changed, removed)
    :ok
  end
end
