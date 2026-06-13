defmodule Realtime.Sequencer do
  @moduledoc """
  Serializes per-organization event sequencing + broadcast so delivery order
  matches sequence order.

  Without this, two same-org events ingested concurrently (e.g. from different
  Pub/Sub pipelines) could be assigned seq 5 and 6 but broadcast 6-before-5; a
  client that disconnected in between would resume from 6 and silently miss 5.

  A small pool of workers, partitioned by org id, keeps strict per-org ordering
  while letting different orgs run in parallel. Each worker handles one event at
  a time: assign the sequence + buffer it (`Realtime.EventLog`), then broadcast to
  the org PubSub topic. Publishing is async (cast), so ingest never blocks.
  """

  use Supervisor

  @pool_size 8

  def start_link(_opts), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    children =
      for i <- 0..(@pool_size - 1) do
        Supervisor.child_spec({Realtime.Sequencer.Worker, i}, id: {Realtime.Sequencer.Worker, i})
      end

    Supervisor.init(children, strategy: :one_for_one)
  end

  @doc """
  Sequence, buffer, and broadcast an org event, preserving per-org order. Async.
  """
  def publish(org_id, event) when is_binary(org_id) and is_map(event) do
    worker = Realtime.Sequencer.Worker.name(:erlang.phash2(org_id, @pool_size))
    GenServer.cast(worker, {:publish, org_id, event})
  end
end

defmodule Realtime.Sequencer.Worker do
  @moduledoc false

  use GenServer

  def name(index), do: :"realtime_sequencer_#{index}"

  def start_link(index), do: GenServer.start_link(__MODULE__, index, name: name(index))

  @impl true
  def init(_index), do: {:ok, %{}}

  @impl true
  def handle_cast({:publish, org_id, event}, state) do
    event = Realtime.EventLog.stamp(org_id, event)
    Phoenix.PubSub.broadcast(Realtime.PubSub, "org:#{org_id}", {:pubsub_event, event})
    {:noreply, state}
  end
end
