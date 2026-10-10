defmodule RealtimeWeb.AccountChannel do
  @moduledoc """
  Channel for email account-specific events.

  Users can subscribe to email account updates (sync status, errors, warmup progress).
  Authorization is checked via organization membership before allowing joins.
  """

  use Phoenix.Channel

  require Logger

  alias Realtime.Auth
  alias Realtime.RateLimiter
  alias RealtimeWeb.ChannelGuard

  @impl true
  def join("account:" <> account_id, _params, socket) do
    # Throttle before the access lookup: that query is what a rejoin loop turns
    # into database load.
    case ChannelGuard.check_join(socket) do
      {:error, payload} -> {:error, payload}
      :ok -> authorize_join(account_id, socket)
    end
  end

  defp authorize_join(account_id, socket) do
    if valid_uuid?(account_id) do
      user_id = socket.assigns.user_id

      # Verify user has access to this email account via organization membership
      checked_at = System.monotonic_time(:millisecond)

      case Auth.check_email_account_access(user_id, account_id) do
        {:ok, member} ->
          Logger.debug("User #{user_id} joined account:#{account_id}")

          socket =
            socket
            |> assign(:account_id, account_id)
            |> ChannelGuard.remember_authorization(member, checked_at)

          send(self(), :after_join)
          {:ok, socket}

        {:error, :not_found} ->
          ChannelGuard.join_error("account_not_found")

        {:error, :forbidden} ->
          ChannelGuard.join_error("forbidden")

        {:error, :not_a_member} ->
          ChannelGuard.join_error("not_a_member")

        {:error, reason} ->
          Logger.warning("Failed to join account channel: #{inspect(reason)}")
          ChannelGuard.join_error(to_string(reason))
      end
    else
      ChannelGuard.join_error("invalid_account_id", :invalid_topic)
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
        Auth.check_email_account_access(socket.assigns.user_id, socket.assigns.account_id)
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
      # Sync events require manage emails permission
      "sync_started" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_emails))

      "sync_completed" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_emails))

      "sync_error" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_emails))

      # Warmup events
      "warmup_progress" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_emails))

      # Default: allow if user has manage emails permission
      _ ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_emails))
    end
  end

  defp handle_client_event(_event, _payload, socket) do
    {:noreply, socket}
  end
end
