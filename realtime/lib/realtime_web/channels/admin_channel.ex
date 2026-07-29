defmodule RealtimeWeb.AdminChannel do
  @moduledoc """
  Platform-wide event firehose for the internal admin dashboard.

  Every event the broadcaster routes (org, user, and entity scoped) is
  mirrored onto the `admin:platform` PubSub topic; this channel forwards it
  to connected admins unfiltered, so the admin app can render a live event
  feed and invalidate its queries without polling.

  Join is gated hard: JWT sockets only (no API keys) and the user must have
  `users.admin_permissions > 0`. There is no presence tracking here — this
  is an internal operations surface, not a collaboration space.
  """

  use Phoenix.Channel

  require Logger

  alias Realtime.Auth
  alias Realtime.RateLimiter

  @topic "admin:platform"

  # Admins drink from the platform firehose, so the per-minute outbound
  # budget is far above the org-channel default. It still exists as a
  # backstop against a runaway event storm saturating one socket.
  @admin_message_limit 3_000

  @impl true
  def join("admin:platform", _params, socket) do
    user_id = socket.assigns.user_id

    if Map.get(socket.assigns, :auth_type) != :jwt do
      {:error, %{reason: "jwt_required"}}
    else
      case Auth.check_admin(user_id) do
        {:ok, admin} ->
          Logger.debug("Admin #{user_id} joined the platform event channel")

          socket = assign(socket, :admin_permissions, Map.get(admin, :permissions, 0))
          send(self(), :after_join)

          {:ok,
           %{
             heartbeat_interval_ms: 25_000,
             server_timeout_ms: 60_000
           }, socket}

        {:error, reason} ->
          Logger.warning("Admin channel join rejected for #{user_id}: #{inspect(reason)}")
          {:error, %{reason: "not_an_admin"}}
      end
    end
  end

  @impl true
  def handle_info(:after_join, socket) do
    Phoenix.PubSub.subscribe(Realtime.PubSub, @topic)
    {:noreply, socket}
  end

  @impl true
  def handle_info({:pubsub_event, event}, socket) do
    user_id = socket.assigns.user_id

    case RateLimiter.check(user_id, :ws_message, @admin_message_limit) do
      {:ok, _remaining} ->
        push(socket, Map.get(event, "event_type") || "event", event)

      {:error, :rate_limited, retry_after_ms} ->
        push(socket, "rate_limited", %{
          category: "ws_message",
          retry_after_ms: retry_after_ms
        })
    end

    {:noreply, socket}
  end

  # Swallow duplicate %Broadcast{} structs the manual PubSub subscription
  # can deliver (same reason as OrgChannel).
  @impl true
  def handle_info(%Phoenix.Socket.Broadcast{}, socket), do: {:noreply, socket}

  @impl true
  def handle_info(_msg, socket), do: {:noreply, socket}
end
