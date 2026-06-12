defmodule Realtime.Redis.EventSubscriber do
  @moduledoc """
  Subscribes to the Redis pub/sub channel the Go backend and consumer publish
  realtime events on, and fans each event out to Phoenix topics via
  `Realtime.EventBroadcaster`.

  This is the transport bridge for local dev and any non-GCP environment. In
  Google Pub/Sub environments the Broadway subscriber does the same job and this
  process is not started, so events are never delivered twice. Redix.PubSub
  reconnects and re-subscribes automatically, so a Redis blip self-heals.
  """

  use GenServer

  require Logger

  alias Realtime.EventBroadcaster

  @channel "realtime:events"

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    redis_url = Application.get_env(:realtime, :redis_url, "redis://localhost:6379/0")

    case Redix.PubSub.start_link(redis_url) do
      {:ok, conn} ->
        {:ok, _ref} = Redix.PubSub.subscribe(conn, @channel, self())
        Logger.info("Realtime Redis event bridge subscribing to '#{@channel}'")
        {:ok, %{conn: conn}}

      {:error, reason} ->
        # Fail open: realtime is a nicety, not a hard dependency. Retry shortly.
        Logger.warning("Redis event bridge connect failed: #{inspect(reason)}; retrying")
        Process.send_after(self(), :retry_connect, 2_000)
        {:ok, %{conn: nil}}
    end
  end

  @impl true
  def handle_info(:retry_connect, %{conn: nil}) do
    {:ok, state} = init([])
    {:noreply, state}
  end

  def handle_info({:redix_pubsub, _conn, _ref, :subscribed, %{channel: channel}}, state) do
    Logger.debug("Redis event bridge subscribed to #{channel}")
    {:noreply, state}
  end

  def handle_info({:redix_pubsub, _conn, _ref, :message, %{payload: payload}}, state) do
    case Jason.decode(payload) do
      {:ok, event} ->
        EventBroadcaster.broadcast(event)

      {:error, reason} ->
        Logger.error("Redis event decode error: #{inspect(reason)}")
    end

    {:noreply, state}
  end

  def handle_info({:redix_pubsub, _conn, _ref, :disconnected, _meta}, state) do
    Logger.warning("Redis event bridge disconnected; Redix will reconnect")
    {:noreply, state}
  end

  def handle_info(_msg, state), do: {:noreply, state}
end
