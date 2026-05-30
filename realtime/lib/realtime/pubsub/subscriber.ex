defmodule Realtime.CloudPubSub.Subscriber do
  @moduledoc """
  Broadway pipeline for consuming Google Pub/Sub messages.

  Receives events from the Go backend and broadcasts them to
  connected WebSocket clients via Phoenix PubSub.
  """

  use Broadway

  require Logger
  alias Realtime.ErrorReporter

  def start_link(opts) do
    subscription = Keyword.fetch!(opts, :subscription)
    project_id = Application.get_env(:realtime, :gcp_project_id)

    Broadway.start_link(__MODULE__,
      name: String.to_atom("pubsub_#{subscription}"),
      producer: [
        module: {
          BroadwayCloudPubSub.Producer,
          subscription: "projects/#{project_id}/subscriptions/#{subscription}", on_failure: :nack
        },
        concurrency: 1
      ],
      processors: [
        default: [
          concurrency: 2
        ]
      ],
      batchers: [
        default: [
          batch_size: 10,
          batch_timeout: 100
        ]
      ]
    )
  end

  @impl true
  def handle_message(_processor, message, _context) do
    case Jason.decode(message.data) do
      {:ok, event} ->
        broadcast_event(event)
        message

      {:error, reason} ->
        Logger.error("Failed to decode Pub/Sub message: #{inspect(reason)}")

        ErrorReporter.capture_message("Pub/Sub decode error",
          extra: %{reason: reason, data: message.data}
        )

        Broadway.Message.failed(message, "json_decode_error")
    end
  rescue
    e ->
      Logger.error("Error handling Pub/Sub message: #{inspect(e)}")
      ErrorReporter.capture_exception(e, extra: %{data: message.data})
      Broadway.Message.failed(message, "processing_error")
  end

  @impl true
  def handle_batch(_batcher, messages, _batch_info, _context) do
    messages
  end

  @impl true
  def handle_failed(messages, _context) do
    Enum.each(messages, fn %{data: data, status: status} ->
      Logger.error("Message failed: #{inspect(status)}, data: #{inspect(data)}")
    end)

    messages
  end

  defp broadcast_event(event) do
    user_id = event["user_id"]
    event_type = event["event_type"]

    # Broadcast to user channel
    if user_id do
      topic = "user:#{user_id}"
      Phoenix.PubSub.broadcast(Realtime.PubSub, topic, {:pubsub_event, event})
      Logger.debug("Broadcast #{event_type} to #{topic}")
    end

    org_id = event["org_id"] || event["organization_id"]

    if org_id do
      topic = "org:#{org_id}"
      Phoenix.PubSub.broadcast(Realtime.PubSub, topic, {:pubsub_event, event})
      Logger.debug("Broadcast #{event_type} to #{topic}")
    end

    # Broadcast to entity-specific channels
    broadcast_to_entity_channels(event)
  end

  defp broadcast_to_entity_channels(event) do
    # Campaign events
    if campaign_id = event["campaign_id"] do
      topic = "campaign:#{campaign_id}"
      Phoenix.PubSub.broadcast(Realtime.PubSub, topic, {:pubsub_event, event})
    end

    # Account events
    if account_id = event["email_account_id"] do
      topic = "account:#{account_id}"
      Phoenix.PubSub.broadcast(Realtime.PubSub, topic, {:pubsub_event, event})
    end

    # Bulk operation events
    if operation_id = event["operation_id"] do
      topic = "bulk:#{operation_id}"
      Phoenix.PubSub.broadcast(Realtime.PubSub, topic, {:pubsub_event, event})
    end
  end
end
