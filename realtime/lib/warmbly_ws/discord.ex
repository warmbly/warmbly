defmodule WarmblyWs.Discord do
  @moduledoc "Minimal Discord webhook logger."

  @doc "Fire-and-forget POST to Discord."
  def notify(message, level \\ :error) do
    payload = %{
      username: "warmbly-ws",
      embeds: [
        %{
          title: "📡 #{String.upcase(to_string(level))}",
          description: String.slice(message, 0, 2048),
          color: color(level),
          timestamp: DateTime.utc_now() |> DateTime.to_iso8601()
        }
      ]
    }

    Task.start(fn ->
      url = Application.get_env(:warmbly_ws, :discord_webhook_url)
      headers = [{"content-type", "application/json"}]
      HTTPoison.post(url, Jason.encode!(payload), headers)
    end)
  end

  defp color(:error), do: 16_011_520
  defp color(:warn),  do: 16_776_960
  defp color(_),      do: 3_091_279
end
