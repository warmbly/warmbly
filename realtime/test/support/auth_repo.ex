defmodule Realtime.Test.AuthRepo do
  use Agent

  def start_link(_opts),
    do: Agent.start_link(fn -> %{handler: nil, queries: []} end, name: __MODULE__)

  def respond_with(handler), do: Agent.update(__MODULE__, &Map.put(&1, :handler, handler))

  def queries, do: Agent.get(__MODULE__, &Enum.reverse(&1.queries))

  def query(sql, params) do
    if Process.whereis(__MODULE__) do
      handler =
        Agent.get_and_update(__MODULE__, fn state ->
          {state.handler, %{state | queries: [{sql, params} | state.queries]}}
        end)

      handler.(sql, params)
    else
      {:error, :database_unavailable}
    end
  end
end
