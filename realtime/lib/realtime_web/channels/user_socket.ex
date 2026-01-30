defmodule RealtimeWeb.UserSocket do
  @moduledoc """
  WebSocket handler with authentication and connection management.

  Supports both JWT tokens and API keys (prefixed with `wmbly_`).
  Implements Discord-style error codes for rejection reasons.
  Rate limits are based on user's subscription tier.
  """

  use Phoenix.Socket

  require Logger

  alias Realtime.Auth
  alias Realtime.Connections
  alias Realtime.RateLimiter
  alias Realtime.Subscription

  # Channels
  channel "user:*", RealtimeWeb.UserChannel
  channel "campaign:*", RealtimeWeb.CampaignChannel
  channel "account:*", RealtimeWeb.AccountChannel
  channel "bulk:*", RealtimeWeb.BulkChannel
  channel "org:*", RealtimeWeb.OrgChannel

  @impl true
  def connect(%{"token" => token}, socket, connect_info) do
    ip = get_ip(connect_info)

    with {:ok, user_id, auth_type} <- Auth.verify_token(token, ip: ip),
         limits <- Subscription.get_limits(user_id),
         :ok <- check_connection_limit(user_id, ip, limits),
         :ok <- check_rate_limit(user_id, limits) do
      Logger.debug("Socket connected for user: #{user_id} (auth: #{auth_type})")

      socket =
        socket
        |> assign(:user_id, user_id)
        |> assign(:ip_address, ip)
        |> assign(:auth_type, auth_type)
        |> assign(:rate_limits, limits)

      {:ok, socket}
    else
      {:error, reason} ->
        code = Auth.error_code(reason)
        message = Auth.error_message(reason)
        Logger.warning("Socket connection rejected (#{code}): #{message}")
        :error

      {:error, :rate_limited, retry_after_ms} ->
        Logger.warning("Socket connection rate limited, retry after #{retry_after_ms}ms")
        :error
    end
  end

  def connect(_params, _socket, _connect_info) do
    Logger.warning("Socket connection rejected: missing token")
    :error
  end

  @impl true
  def id(socket), do: "user_socket:#{socket.assigns.user_id}"

  # Private functions

  defp check_connection_limit(user_id, ip, limits) do
    max_connections = Map.get(limits, :max_connections, 10)

    case Connections.track(user_id, ip: ip, max_connections: max_connections) do
      :ok -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  defp check_rate_limit(user_id, limits) do
    limit = Map.get(limits, :limit_ws_join_pm, 30)

    case RateLimiter.check(user_id, :ws_join, limit) do
      {:ok, _remaining} -> :ok
      {:error, :rate_limited, retry_after_ms} -> {:error, :rate_limited, retry_after_ms}
    end
  end

  defp get_ip(%{peer_data: %{address: address}}) do
    address |> :inet.ntoa() |> to_string()
  end

  defp get_ip(%{x_headers: headers}) do
    headers
    |> Enum.find(fn {key, _} -> key == "x-forwarded-for" end)
    |> case do
      {_, ip} -> ip |> String.split(",") |> List.first() |> String.trim()
      nil -> "unknown"
    end
  end

  defp get_ip(_), do: "unknown"
end
