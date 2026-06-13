defmodule Realtime.EventBroadcaster do
  @moduledoc """
  Fans a backend event out to the matching Phoenix PubSub topics: the actor's
  user channel, the org channel, and any entity channels (campaign / account /
  bulk). Routing is purely by event body fields, so the source transport (Google
  Pub/Sub via Broadway, or Redis pub/sub in dev/non-GCP envs) does not matter.
  """

  require Logger

  @doc """
  Broadcast a decoded event map. Unknown shapes are ignored.
  """
  def broadcast(event) when is_map(event) do
    user_id = event["user_id"]
    event_type = event["event_type"]

    if present?(user_id) do
      Phoenix.PubSub.broadcast(Realtime.PubSub, "user:#{user_id}", {:pubsub_event, event})
    end

    org_id = event["org_id"] || event["organization_id"]

    if present?(org_id) do
      # Route org events through the sequencer so each org's events are assigned a
      # monotonic seq, buffered for replay, and broadcast IN ORDER (even when
      # ingested concurrently) — the invariant resume relies on.
      Realtime.Sequencer.publish(org_id, event)
    end

    broadcast_to_entity_channels(event)
    Logger.debug("Broadcast #{event_type}")
    :ok
  end

  def broadcast(_), do: :ok

  defp broadcast_to_entity_channels(event) do
    if campaign_id = event["campaign_id"] do
      Phoenix.PubSub.broadcast(Realtime.PubSub, "campaign:#{campaign_id}", {:pubsub_event, event})
    end

    if account_id = event["email_account_id"] do
      Phoenix.PubSub.broadcast(Realtime.PubSub, "account:#{account_id}", {:pubsub_event, event})
    end

    if operation_id = event["operation_id"] do
      Phoenix.PubSub.broadcast(Realtime.PubSub, "bulk:#{operation_id}", {:pubsub_event, event})
    end

    :ok
  end

  defp present?(value), do: is_binary(value) and value != ""
end
