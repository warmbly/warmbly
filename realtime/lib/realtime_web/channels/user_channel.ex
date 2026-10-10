defmodule RealtimeWeb.UserChannel do
  @moduledoc """
  Channel for user-specific events.

  Users automatically join their personal channel on socket connection.
  Events include: email received, account status changes, bulk operation progress.

  Implements rate limiting for outbound messages and client events.
  """

  use Phoenix.Channel

  require Logger

  alias Realtime.Auth
  alias Realtime.Connections
  alias Realtime.RateLimiter
  alias RealtimeWeb.ChannelGuard
  alias RealtimeWeb.OrgChannel

  @authorization_cache_limit 128

  @impl true
  def join("user:" <> user_id, _params, socket) do
    case ChannelGuard.check_join(socket) do
      {:error, payload} ->
        {:error, payload}

      :ok ->
        # Users can only join their own channel
        if socket.assigns.user_id == user_id do
          Logger.debug("User #{user_id} joined user channel")
          {:ok, socket}
        else
          ChannelGuard.join_error("unauthorized")
        end
    end
  end

  @impl true
  def handle_info({:pubsub_event, event}, socket) do
    {allowed?, socket} = authorize_event(socket, event)

    if allowed?, do: deliver(socket, event), else: {:noreply, socket}
  end

  defp deliver(socket, event) do
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

  defp authorize_event(socket, event) do
    case authorization_key(event) do
      :personal ->
        {true, socket}

      :unscoped ->
        {false, socket}

      key ->
        now = System.monotonic_time(:millisecond)
        cache = Map.get(socket.assigns, :event_authorizations, %{})

        case Map.get(cache, key) do
          {checked_at, member} ->
            if ChannelGuard.fresh?(checked_at, now),
              do: {can_see_event?(socket, member, event), socket},
              else: reread_event_authorization(socket, event, key, cache, now)

          nil ->
            reread_event_authorization(socket, event, key, cache, now)
        end
    end
  end

  defp reread_event_authorization(socket, event, key, cache, now) do
    case lookup_event_authorization(socket.assigns.user_id, key) do
      {:ok, member} ->
        if ChannelGuard.fresh?(now, System.monotonic_time(:millisecond)) do
          cache =
            cache
            |> Enum.filter(fn {_key, {checked_at, _member}} ->
              ChannelGuard.fresh?(checked_at, now)
            end)
            |> Map.new()

          cache = if map_size(cache) >= @authorization_cache_limit, do: %{}, else: cache
          socket = assign(socket, :event_authorizations, Map.put(cache, key, {now, member}))
          {can_see_event?(socket, member, event), socket}
        else
          {false, assign(socket, :event_authorizations, Map.delete(cache, key))}
        end

      {:error, _reason} ->
        {false, assign(socket, :event_authorizations, Map.delete(cache, key))}
    end
  end

  defp can_see_event?(socket, member, event) do
    event =
      if is_binary(member[:event_campaign_id]),
        do: Map.put(event, "campaign_id", member.event_campaign_id),
        else: event

    Auth.matches_org?(member, event["org_id"] || event["authorization_org_id"]) and
      socket
      |> ChannelGuard.remember_authorization(member)
      |> OrgChannel.can_see_event?(event)
  end

  defp authorization_key(event) do
    cond do
      event["event_type"] == "NOTIFICATION_CREATED" ->
        resource_key(:notification, event["notification_id"])

      is_binary(event["campaign_id"]) and event["campaign_id"] != "" ->
        {:campaign, event["campaign_id"]}

      is_binary(event["email_account_id"]) and event["email_account_id"] != "" ->
        {:account, event["email_account_id"]}

      is_binary(event["booking_id"]) and event["booking_id"] != "" ->
        {:booking, event["booking_id"]}

      is_binary(event["org_id"]) and event["org_id"] != "" ->
        {:org, event["org_id"]}

      is_binary(event["authorization_org_id"]) and event["authorization_org_id"] != "" ->
        {:org, event["authorization_org_id"]}

      is_binary(event["contact_id"]) and event["contact_id"] != "" ->
        {:contact, event["contact_id"]}

      event["event_type"] in ["SESSIONS_REVOKED", "USER_UPDATED", "USER_PREFERENCES_UPDATED"] ->
        :personal

      true ->
        :unscoped
    end
  end

  defp resource_key(kind, id) when is_binary(id) and id != "", do: {kind, id}
  defp resource_key(_kind, _id), do: :unscoped

  defp lookup_event_authorization(user_id, {:org, id}), do: Auth.check_org_membership(user_id, id)

  defp lookup_event_authorization(user_id, {:campaign, id}),
    do: Auth.check_campaign_access(user_id, id)

  defp lookup_event_authorization(user_id, {:account, id}),
    do: Auth.check_email_account_access(user_id, id)

  defp lookup_event_authorization(user_id, {kind, id}),
    do: Auth.check_user_event_resource(user_id, kind, id)

  defp handle_client_event(_event, _payload, socket) do
    # Default handler for unknown events
    {:noreply, socket}
  end
end
