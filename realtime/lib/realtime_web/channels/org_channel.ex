defmodule RealtimeWeb.OrgChannel do
  @moduledoc """
  Channel for organization-specific events.

  Users can join their organization's channel to receive events like:
  - member_joined: New member joined the organization
  - member_left: Member left or was removed
  - member_role_changed: Member's role/permissions changed
  - settings_changed: Organization settings updated
  - subscription_changed: Subscription status changed

  Authorization is handled by checking organization membership.
  """

  use Phoenix.Channel

  require Logger

  alias Realtime.Auth
  alias Realtime.Connections
  alias Realtime.RateLimiter

  @impl true
  def join("org:" <> org_id, _params, socket) do
    user_id = socket.assigns.user_id

    case Auth.check_org_membership(user_id, org_id) do
      {:ok, member} ->
        Logger.debug("User #{user_id} joined org channel #{org_id}")

        socket =
          socket
          |> assign(:org_id, org_id)
          |> assign(:member, member)
          |> assign(:permissions, Map.get(member, :permissions, 0))

        send(self(), :after_join)
        {:ok, %{org_id: org_id, role: member.role}, socket}

      {:error, :not_a_member} ->
        {:error, %{reason: "not_a_member"}}

      {:error, reason} ->
        Logger.warning("Failed to join org channel: #{inspect(reason)}")
        {:error, %{reason: to_string(reason)}}
    end
  end

  @impl true
  def handle_info(:after_join, socket) do
    # Subscribe to the organization's Pub/Sub topic
    org_id = socket.assigns.org_id
    Phoenix.PubSub.subscribe(Realtime.PubSub, "org:#{org_id}")
    {:noreply, socket}
  end

  @impl true
  def handle_info({:pubsub_event, event}, socket) do
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

  @impl true
  def terminate(_reason, socket) do
    ip = Map.get(socket.assigns, :ip_address)
    Connections.untrack(socket.assigns.user_id, ip: ip)
    :ok
  end

  # Private functions

  # Check if user has permission to see a specific event type
  defp can_see_event?(socket, event) do
    event_type = Map.get(event, "event_type", "")
    permissions = socket.assigns.permissions

    case event_type do
      # Billing events require billing permission
      "subscription_changed" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_billing))

      # Member events require team management permission
      "member_joined" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_team))

      "member_left" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_team))

      "member_role_changed" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_team))

      # Settings changes require settings permission
      "settings_changed" ->
        Auth.has_permission?(%{permissions: permissions}, Auth.permission(:manage_settings))

      # Default: allow all other events
      _ ->
        true
    end
  end

  defp handle_client_event(_event, _payload, socket) do
    # Default handler for unknown events
    {:noreply, socket}
  end
end
