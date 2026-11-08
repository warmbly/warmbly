defmodule RealtimeWeb.AdminChannel do
  @moduledoc """
  Platform-wide event firehose for the internal admin dashboard.

  Every event the broadcaster routes (org, user, and entity scoped) is
  mirrored onto the `admin:platform` PubSub topic; this channel forwards it
  to connected admins unfiltered, so the admin app can render a live event
  feed and invalidate its queries without polling.

  Join is gated hard: JWT sockets only (no API keys), from a session that
  verified a second factor, and the user must have
  `users.admin_permissions > 0`. There is no presence tracking here — this
  is an internal operations surface, not a collaboration space.
  """

  use Phoenix.Channel

  require Logger

  alias Realtime.Auth
  alias Realtime.RateLimiter
  alias RealtimeWeb.ChannelGuard

  # Admins drink from the platform firehose, so the per-minute outbound
  # budget is far above the org-channel default. It still exists as a
  # backstop against a runaway event storm saturating one socket.
  @admin_message_limit 3_000

  @impl true
  def join("admin:platform", _params, socket) do
    case ChannelGuard.check_join(socket) do
      {:error, payload} -> {:error, payload}
      :ok -> authorize_join(socket)
    end
  end

  defp authorize_join(socket) do
    user_id = socket.assigns.user_id

    cond do
      Map.get(socket.assigns, :auth_type) != :jwt ->
        ChannelGuard.join_error("jwt_required")

      # Same rule as the admin API: only a session that verified a second factor.
      Map.get(socket.assigns, :mfa_verified) != true ->
        ChannelGuard.join_error("admin_mfa_required")

      # A session signed in on an agency portal never reaches the operator surface.
      not is_nil(Map.get(socket.assigns, :portal_agency_id)) ->
        ChannelGuard.join_error("portal_session_restricted")

      true ->
        authorize_admin(socket, user_id)
    end
  end

  defp authorize_admin(socket, user_id) do
    case Auth.check_admin(user_id) do
      {:ok, admin} ->
        Logger.debug("Admin #{user_id} joined the platform event channel")

        socket = assign(socket, :admin_permissions, Map.get(admin, :permissions, 0))

        {:ok,
         %{
           heartbeat_interval_ms: 25_000,
           server_timeout_ms: 60_000
         }, socket}

      {:error, reason} ->
        Logger.warning("Admin channel join rejected for #{user_id}: #{inspect(reason)}")
        ChannelGuard.join_error("not_an_admin")
    end
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

  # Socket control broadcasts are handled by Phoenix, not forwarded as events.
  @impl true
  def handle_info(%Phoenix.Socket.Broadcast{}, socket), do: {:noreply, socket}

  @impl true
  def handle_info(_msg, socket), do: {:noreply, socket}
end
