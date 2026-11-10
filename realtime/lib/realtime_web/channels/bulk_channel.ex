defmodule RealtimeWeb.BulkChannel do
  @moduledoc """
  Channel for bulk operation events.

  Users can subscribe to bulk operation progress (contact imports, bulk updates).

  A bulk operation id is not backed by a table the realtime service can read, so
  the join itself cannot prove ownership. Delivery is gated instead: an event is
  pushed only to the socket of the user who owns it (`user_id` on the event
  body, set by the publisher). Joining someone else's operation id therefore
  yields an open channel that never receives anything, rather than a live feed of
  another account's import.
  """

  use Phoenix.Channel

  require Logger

  alias Realtime.Auth
  alias RealtimeWeb.ChannelGuard
  alias RealtimeWeb.OrgChannel

  @impl true
  def join("bulk:" <> operation_id, _params, socket) do
    case ChannelGuard.check_join(socket) do
      {:error, payload} -> {:error, payload}
      :ok -> subscribe(operation_id, socket)
    end
  end

  defp subscribe(operation_id, socket) do
    if valid_uuid?(operation_id) do
      Logger.debug("User #{socket.assigns.user_id} joined bulk:#{operation_id}")
      {:ok, assign(socket, :operation_id, operation_id)}
    else
      ChannelGuard.join_error("invalid_operation_id", :invalid_topic)
    end
  end

  @impl true
  def handle_info({:pubsub_event, event}, socket) do
    if own_event?(event, socket) do
      push(socket, event["event_type"], event)
    end

    {:noreply, socket}
  end

  @impl true
  def handle_in("ping", _payload, socket) do
    {:reply, {:ok, %{pong: System.system_time(:millisecond)}}, socket}
  end

  @doc false
  # An event with no owner belongs to nobody and is never delivered here.
  def own_event?(%{"user_id" => user_id} = event, socket)
      when is_binary(user_id) and user_id != "" do
    user_id == socket.assigns.user_id and credential_reaches?(event, socket) and
      portal_reaches?(event, socket)
  end

  def own_event?(_event, _socket), do: false

  # An API key or OAuth token sees only its own workspace's operations, within its scopes.
  defp credential_reaches?(event, socket) do
    case socket.assigns[:credential] do
      nil ->
        true

      %{organization_id: credential_org, permissions: mask} ->
        org_id = event["org_id"] || event["organization_id"]

        is_binary(org_id) and org_id != "" and
          Auth.matches_org?(%{organization_id: credential_org}, org_id) and
          socket
          |> ChannelGuard.remember_authorization(%{permissions: mask})
          |> OrgChannel.can_see_event?(event)
    end
  end

  @portal_check_ttl_ms 30_000

  # A portal session sees only operations in its agency's client workspaces, rechecked every 30 seconds.
  defp portal_reaches?(event, socket) do
    case socket.assigns[:portal_agency_id] do
      nil ->
        true

      agency_id ->
        org_id = event["org_id"] || event["organization_id"]
        is_binary(org_id) and org_id != "" and portal_client?(org_id, agency_id)
    end
  end

  defp portal_client?(org_id, agency_id) do
    now = System.monotonic_time(:millisecond)
    key = {:portal_client, org_id}

    case Process.get(key) do
      {checked_at, allowed} when now - checked_at < @portal_check_ttl_ms ->
        allowed

      _ ->
        allowed = Auth.agency_client?(org_id, agency_id) == {:ok, true}
        Process.put(key, {now, allowed})
        allowed
    end
  end

  defp valid_uuid?(id) do
    case UUID.info(id) do
      {:ok, _} -> true
      _ -> false
    end
  end
end
