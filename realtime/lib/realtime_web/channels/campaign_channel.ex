defmodule RealtimeWeb.CampaignChannel do
  @moduledoc """
  Channel for campaign-specific events.

  Users can subscribe to campaign updates (progress, status changes).
  Authorization is checked via organization membership before allowing joins.
  """

  use Phoenix.Channel

  require Logger

  alias Realtime.Auth
  alias Realtime.RateLimiter
  alias RealtimeWeb.ChannelGuard

  @impl true
  def join("campaign:" <> campaign_id, _params, socket) do
    # Throttle before the access lookup: that query is what a rejoin loop turns
    # into database load.
    case ChannelGuard.check_join(socket) do
      {:error, payload} -> {:error, payload}
      :ok -> authorize_join(campaign_id, socket)
    end
  end

  defp authorize_join(campaign_id, socket) do
    if valid_uuid?(campaign_id) do
      user_id = socket.assigns.user_id

      # Verify user has access to this campaign via organization membership
      checked_at = System.monotonic_time(:millisecond)

      case Auth.check_campaign_access(user_id, campaign_id) do
        {:ok, member} ->
          Logger.debug("User #{user_id} joined campaign:#{campaign_id}")

          socket =
            socket
            |> assign(:campaign_id, campaign_id)
            |> ChannelGuard.remember_authorization(member, checked_at)

          send(self(), :after_join)
          {:ok, socket}

        {:error, :not_found} ->
          ChannelGuard.join_error("campaign_not_found")

        {:error, :forbidden} ->
          ChannelGuard.join_error("forbidden")

        {:error, :not_a_member} ->
          ChannelGuard.join_error("not_a_member")

        {:error, reason} ->
          Logger.warning("Failed to join campaign channel: #{inspect(reason)}")
          ChannelGuard.join_error(to_string(reason))
      end
    else
      ChannelGuard.join_error("invalid_campaign_id", :invalid_topic)
    end
  end

  @impl true
  def handle_info(:after_join, socket) do
    ChannelGuard.schedule_authorization_refresh(socket)
    {:noreply, socket}
  end

  @impl true
  def handle_info({:pubsub_event, event}, socket) do
    case refresh_authorization(socket) do
      {:ok, socket} -> deliver(socket, event)
      {:error, _reason} -> {:stop, :normal, socket}
    end
  end

  def handle_info(:refresh_authorization, socket) do
    case refresh_authorization(socket, true) do
      {:ok, socket} ->
        ChannelGuard.schedule_authorization_refresh(socket)
        {:noreply, socket}

      {:error, _reason} ->
        {:stop, :normal, socket}
    end
  end

  defp refresh_authorization(socket, force \\ false) do
    ChannelGuard.refresh_authorization(
      socket,
      fn ->
        Auth.check_campaign_access(socket.assigns.user_id, socket.assigns.campaign_id)
      end,
      force
    )
  end

  defp deliver(socket, event) do
    # Rate limit outbound messages
    user_id = socket.assigns.user_id
    limits = Map.get(socket.assigns, :rate_limits, %{})
    ws_message_limit = Map.get(limits, :limit_ws_message_pm)

    case RateLimiter.check(user_id, :ws_message, ws_message_limit) do
      {:ok, _remaining} ->
        # Check if user has permission to see this event
        if can_see_event?(socket, event) do
          push(socket, event["event_type"], event)
        end

      {:error, :rate_limited, retry_after_ms} ->
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
    # Rate limit client-sent events
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

  # Private functions

  defp valid_uuid?(id) do
    case UUID.info(id) do
      {:ok, _} -> true
      _ -> false
    end
  end

  # Check if user has permission to see a specific event type
  defp can_see_event?(socket, event) do
    event_type = Map.get(event, "event_type", "")
    permissions = socket.assigns.permissions

    case event_type do
      # Analytics events require view analytics permission
      "campaign_stats" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:view_analytics))

      # Status changes - viewers can see
      "campaign_status_changed" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:view_campaigns))

      # Progress events - viewers can see
      "campaign_progress" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:view_campaigns))

      # Email sent events
      "email_sent" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:view_campaigns))

      # Task progress events - detailed task updates
      "task_progress" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:view_campaigns))

      # Email tracking events
      "email_opened" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:view_campaigns))

      "email_clicked" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:view_campaigns))

      "email_bounced" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:view_campaigns))

      # Email reply events - requires unibox access
      "email_reply" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:access_unibox))

      "email_replied" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:access_unibox))

      # Default: allow if user has view campaigns permission
      _ ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:view_campaigns))
    end
  end

  defp handle_client_event(_event, _payload, socket) do
    # Default handler for unknown events
    {:noreply, socket}
  end
end
