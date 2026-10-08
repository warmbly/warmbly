defmodule RealtimeWeb.UserSocket do
  @moduledoc """
  WebSocket handler with authentication and connection management.

  Accepts the dashboard's ws ticket in the `token` query parameter, and API
  keys (`wmbly_`) or OAuth tokens (`wmat_`) in the `x-warmbly-token` header.
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
  channel("user:*", RealtimeWeb.UserChannel)
  channel("campaign:*", RealtimeWeb.CampaignChannel)
  channel("account:*", RealtimeWeb.AccountChannel)
  channel("bulk:*", RealtimeWeb.BulkChannel)
  channel("org:*", RealtimeWeb.OrgChannel)
  channel("admin:*", RealtimeWeb.AdminChannel)

  # API keys and OAuth tokens belong in this header; the query string, which
  # proxies record, is a deprecated fallback for them kept for existing clients.
  @credential_header "x-warmbly-token"

  @impl true
  def connect(params, socket, connect_info) do
    case credential(params, connect_info) do
      {:ok, token} -> authenticate(token, socket, connect_info)
      {:error, reason} -> reject(reason)
    end
  end

  @doc false
  def credential(params, connect_info) do
    header = header_credential(connect_info)
    query = Map.get(params, "token")

    cond do
      present?(header) ->
        {:ok, header}

      present?(query) and Auth.long_lived?(query) ->
        Logger.info(
          "Socket credential in the query string; send it in the x-warmbly-token header"
        )

        {:ok, query}

      present?(query) ->
        {:ok, query}

      true ->
        {:error, :missing_token}
    end
  end

  defp header_credential(%{x_headers: headers}) when is_list(headers) do
    Enum.find_value(headers, fn
      {@credential_header, value} -> String.trim(value)
      _ -> nil
    end)
  end

  defp header_credential(_), do: nil

  defp present?(value), do: is_binary(value) and value != ""

  defp authenticate(token, socket, connect_info) do
    ip = client_ip(token, connect_info)

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
      # The handshake rate limiter returns a 3-tuple carrying the cooldown; match
      # it before the generic 2-tuple clause and surface a Retry-After style hint.
      {:error, :rate_limited, retry_after_ms} ->
        reject(:rate_limited, retry_after_ms: retry_after_ms)

      {:error, reason} ->
        reject(reason)
    end
  end

  # Build the structured rejection returned from connect/2.
  #
  # Phoenix.Socket.connect/2 runs before the WebSocket upgrade completes, so we
  # cannot send a real WS close frame here (e.g. a 4007 close); the close code is
  # advisory. Returning {:error, term} hands `term` to the transport's
  # :error_handler (Phoenix.Transports.WebSocket), which can render it; the
  # default handler still replies HTTP 403, so the 403 response carries the
  # reason for transports/handlers that surface it.
  defp reject(reason, extra \\ []) do
    code = Auth.error_code(reason)
    message = Auth.error_message(reason)

    payload =
      %{code: code, reason: message}
      |> maybe_put_retry_after(extra[:retry_after_ms])

    log_rejection(code, message, payload)

    {:error, payload}
  end

  defp maybe_put_retry_after(payload, nil), do: payload

  defp maybe_put_retry_after(payload, retry_after_ms) when is_integer(retry_after_ms) do
    Map.put(payload, :retry_after_ms, retry_after_ms)
  end

  defp maybe_put_retry_after(payload, _), do: payload

  defp log_rejection(code, message, %{retry_after_ms: retry_after_ms}) do
    Logger.warning(
      "Socket connection rejected (#{code}): #{message}, retry after #{retry_after_ms}ms"
    )
  end

  defp log_rejection(code, message, _payload) do
    Logger.warning("Socket connection rejected (#{code}): #{message}")
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

  # Handshake throttle. This spends the `ws_connect` budget, NOT `ws_join`:
  # channel joins have their own bucket (RealtimeWeb.ChannelGuard) so a
  # reconnect storm cannot eat the budget a client then needs to rejoin its
  # topics with.
  defp check_rate_limit(user_id, limits) do
    limit = Map.get(limits, :limit_ws_join_pm, 30)

    case RateLimiter.check(user_id, :ws_connect, limit) do
      {:ok, _remaining} -> :ok
      {:error, :rate_limited, retry_after_ms} -> {:error, :rate_limited, retry_after_ms}
    end
  end

  def client_ip(ticket, connect_info) do
    proof =
      connect_info
      |> Map.get(:x_headers, [])
      |> Enum.find_value(fn
        {"x-warmbly-proxy-proof", value} -> value
        _ -> nil
      end)

    with {:ok, ip} <- Auth.proxy_ip(proof, ticket) do
      ip
    else
      _ -> get_ip(connect_info)
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
