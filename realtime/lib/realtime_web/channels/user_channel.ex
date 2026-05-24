defmodule RealtimeWeb.UserChannel do
  @moduledoc """
  Channel for user-specific events.

  Users automatically join their personal channel on socket connection.
  Events include: email received, account status changes, bulk operation progress.

  Implements rate limiting for outbound messages and client events.
  """

  use Phoenix.Channel

  require Logger

  alias Realtime.Connections
  alias Realtime.RateLimiter

  @impl true
  def join("user:" <> user_id, _params, socket) do
    # Users can only join their own channel
    if socket.assigns.user_id == user_id do
      Logger.debug("User #{user_id} joined user channel")
      send(self(), :after_join)
      {:ok, socket}
    else
      {:error, %{reason: "unauthorized"}}
    end
  end

  @impl true
  def handle_info(:after_join, socket) do
    # Subscribe to the user's Pub/Sub topic
    Phoenix.PubSub.subscribe(Realtime.PubSub, "user:#{socket.assigns.user_id}")
    {:noreply, socket}
  end

  @impl true
  def handle_info({:pubsub_event, event}, socket) do
    # Rate limit outbound messages using subscription-based limits
    user_id = socket.assigns.user_id
    limits = Map.get(socket.assigns, :rate_limits, %{})
    ws_message_limit = Map.get(limits, :limit_ws_message_pm)

    case RateLimiter.check(user_id, :ws_message, ws_message_limit) do
      {:ok, _remaining} ->
        push(socket, event["event_type"], event)

      {:error, :rate_limited, retry_after_ms} ->
        # Send rate limit notification instead of the event
        push(socket, "rate_limited", %{
          category: "ws_message",
          retry_after_ms: retry_after_ms
        })

        Logger.debug("User #{user_id} rate limited on ws_message")
    end

    {:noreply, socket}
  end

  @impl true
  def handle_in("ping", _payload, socket) do
    {:reply, {:ok, %{pong: System.system_time(:millisecond)}}, socket}
  end

  @impl true
  def handle_in(event, payload, socket) do
    # Rate limit client-sent events using subscription-based limits
    user_id = socket.assigns.user_id
    limits = Map.get(socket.assigns, :rate_limits, %{})
    ws_event_limit = Map.get(limits, :limit_ws_event_pm)

    case RateLimiter.check(user_id, :ws_event, ws_event_limit) do
      {:ok, _remaining} ->
        handle_client_event(event, payload, socket)

      {:error, :rate_limited, retry_after_ms} ->
        {:reply,
         {:error,
          %{
            reason: "rate_limited",
            category: "ws_event",
            retry_after_ms: retry_after_ms
          }}, socket}
    end
  end

  @impl true
  def terminate(_reason, socket) do
    ip = Map.get(socket.assigns, :ip_address)
    Connections.untrack(socket.assigns.user_id, ip: ip)
    :ok
  end

  # Private functions

  defp handle_client_event(_event, _payload, socket) do
    # Default handler for unknown events
    {:noreply, socket}
  end
end
