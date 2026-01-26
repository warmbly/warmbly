# lib/warmbly_ws/logger_reporter.ex
defmodule WarmblyWs.LoggerReporter do
  use GenServer
  require Logger

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def init(_) do
    :ok = :logger.add_handler(__MODULE__, __MODULE__, %{level: :error})
    {:ok, %{}}
  end

  def log(event, _config) do
    msg =
      event
      |> inspect(limit: :infinity)
      |> String.slice(0, 2048)

    WarmblyWs.Discord.notify(msg, event.level)
  end
end
