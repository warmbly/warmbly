defmodule WarmblyWs.SocketHandler do
  @behaviour :cowboy_websocket

  @idle_timeout 60_000
  @max_per_user 10
  @conn_ttl 300
  @max_message_size 4096

  alias ExLimit

  def init(req, state) do
    case :cowboy_req.header("upgrade", req) do
      "websocket" ->
        {ip, _port} = :cowboy_req.peer(req)
        ip_string = :inet.ntoa(ip) |> to_string()

        case ExLimit.check_rate("wsrate:" <> ip_string, :redix, 20, 60_000) do
          {:ok, _} ->
            token = :cowboy_req.parse_qs(req) |> Enum.into(%{}) |> Map.get("token")

            case verify_token(token) do
              {:ok, %{user_id: user_id}} ->
                Redix.command(:redix, ["DEL", "ws_verify:" <> token])
                case reserve_slot(user_id) do
                  {:ok, _} ->
                    {:cowboy_websocket, req, %{user_id: user_id, last_ts: 0}, %{compress: true, idle_timeout: @idle_timeout}}
                  {:error, :limit_exceeded} ->
                    {:ok, error_reply(400, "CONNECTION", "Too many connections.", req), state}
                end
              {:error, _} ->
                {:ok, error_reply(401, "Unauthorized", "Invalid Authorization Token.", req), state}
            end
          {:rate_limit, _} ->
            {:ok, error_reply(429, "Rate Limit", "Too many connections tries.", req), state}
          {:error, _} ->
            {:ok, error_reply(500, "SERVER_ERROR", "Internal server error.", req), state}
        end
      _->
        {:ok, :cowboy_req.reply(403, %{}, "", req), :unused}
    end
  end

  def websocket_init(req, state) do
    Redix.command(:redix, ["SUBSCRIBE", "broadcast:#{state.user_id}"])
    {:ok, state, req, @idle_timeout}
  end

  def websocket_handle({:text, message}, state) do
    cond do
      byte_size(message) <= @max_message_size ->
        now = System.system_time(:millisecond)

        if now - state.last_ts < 2000 do
          {:stop, state}
        else
          new_state = %{state | last_ts: now}
          case Jason.decode(message) do
            {:ok, %{"type" => "PING"}} ->
              {:reply, {:text, Jason.encode!(%{type: "PONG", timestamp: now})}, new_state}
            _ ->
              {:ok, state}
            end
        end
      true ->
        {:stop, state}
    end
  end

  def websocket_info({:redix_pubsub, _pid, %{type: type, payload: payload}}, state) do
    case type do
      "logout"->
        {:stop, state}
      _->
        {:reply, {:text, Jason.encode!(%{type: type, payload: payload})}, state}
    end
  end

  def websocket_terminate(_reason, _req, state) do
    release_slot(state.user_id)
    Redix.command(:redix, ["UNSUBSCRIBE", "broadcast:#{state.user_id}"])
    :ok
  end

  defp error_reply(code, type, message, req) do
    :cowboy_req.reply(
      code,
      %{"content-type"=> "application/json"},
      Jason.encode!(%{
        type: type,
        message: message
      }),
    req)
  end

  defp counter_key(user_id), do: "ws:#{user_id}"

  defp reserve_slot(user_id) do
    lua = """
    local key = KEYS[1]
    local max = tonumber(ARGV[1])

    local current = redis.call('GET', key) or 0
    current = tonumber(current)

    if current >= max then
      return 0
    end

    redis.call('INCR', key)
    redis.call('EXPIRE', key, ARGV[2])
    return 1
    """

    case Redix.command(:redix, ["EVAL", lua, 1, counter_key(user_id), @max_per_user, @conn_ttl]) do
      {:ok, 1} -> {:ok, :reserved}
      {:ok, 0} -> {:error, :limit_exceeded}
      err      -> err
    end
  end

  defp release_slot(user_id) do
    Redix.command(:redix, ["DECR", counter_key(user_id)])
  end

  defp verify_token(token) do
    case Redix.command(:redix, ["GET", "ws_verify:" <> token]) do
      {:ok, userid} ->
        {:ok, %{user_id: userid}}
      _->
        {:error, :invalid_token}
    end
  end
end
