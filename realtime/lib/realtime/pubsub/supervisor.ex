defmodule Realtime.CloudPubSub.Supervisor do
  @moduledoc """
  Supervisor for Google Pub/Sub subscribers.

  Starts a Broadway pipeline for each configured subscription.
  """

  use Supervisor

  require Logger

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    enabled = Application.get_env(:realtime, :pubsub_enabled, false)

    children =
      if enabled do
        subscriptions = Application.get_env(:realtime, :pubsub_subscriptions, [])

        Logger.info("Starting Pub/Sub subscribers for: #{inspect(subscriptions)}")

        Enum.map(subscriptions, fn subscription ->
          Supervisor.child_spec(
            {Realtime.CloudPubSub.Subscriber, subscription: subscription},
            id: String.to_atom("pubsub_#{subscription}")
          )
        end)
      else
        Logger.info("Pub/Sub subscribers disabled (PUBSUB_ENABLED != true)")
        []
      end

    Supervisor.init(children, strategy: :one_for_one)
  end
end
