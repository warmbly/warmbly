defmodule RealtimeWeb.BulkChannel do
  @moduledoc """
  Channel for bulk operation events.

  Users can subscribe to bulk operation progress (contact imports, bulk updates).
  """

  use Phoenix.Channel

  require Logger

  @impl true
  def join("bulk:" <> operation_id, _params, socket) do
    if valid_uuid?(operation_id) do
      Logger.debug("User #{socket.assigns.user_id} joined bulk:#{operation_id}")
      Phoenix.PubSub.subscribe(Realtime.PubSub, "bulk:#{operation_id}")
      {:ok, assign(socket, :operation_id, operation_id)}
    else
      {:error, %{reason: "invalid_operation_id"}}
    end
  end

  @impl true
  def handle_info({:pubsub_event, event}, socket) do
    push(socket, event["event_type"], event)
    {:noreply, socket}
  end

  @impl true
  def handle_in("ping", _payload, socket) do
    {:reply, {:ok, %{pong: System.system_time(:millisecond)}}, socket}
  end

  defp valid_uuid?(id) do
    case UUID.info(id) do
      {:ok, _} -> true
      _ -> false
    end
  end
end
