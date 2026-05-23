defmodule Realtime.Connections do
  @moduledoc """
  Tracks active WebSocket connections per user and IP.

  Uses ETS for fast local lookups and Redis for cross-instance tracking.
  Supports configurable limits via environment variables.

  Process-monitoring lifecycle: when `track/2` is called from a socket
  process, the GenServer monitors that process and automatically calls
  `do_untrack/2` on `:DOWN`. This handles the case where the socket
  disconnects without ever joining a channel — channel `terminate`
  callbacks don't run in that path, so we'd otherwise leak the count
  and eventually trip the per-user limit until a restart.
  """

  use GenServer

  require Logger

  alias Realtime.Redis

  @table :realtime_connections
  @ip_table :realtime_ip_connections

  # Default limits (can be overridden via config)
  @default_max_per_user 10
  @default_max_per_ip 50
  @default_max_global 100_000

  # Redis key TTL for distributed counts (10 minutes)
  @redis_ttl 600

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: __MODULE__)
  end

  @doc """
  Track a new connection for a user.
  Returns :ok if allowed, {:error, :limit_exceeded} if too many connections.

  Options:
  - :ip - Client IP address for IP-based limiting
  - :max_connections - Custom max connections limit (subscription-based)
  - :pid - Owning process (defaults to caller); used for monitor-based untrack
  """
  def track(user_id, opts \\ []) do
    ip = Keyword.get(opts, :ip)
    custom_max = Keyword.get(opts, :max_connections)
    pid = Keyword.get(opts, :pid, self())
    GenServer.call(__MODULE__, {:track, user_id, ip, custom_max, pid})
  end

  @doc """
  Untrack a connection for a user.
  """
  def untrack(user_id, opts \\ []) do
    ip = Keyword.get(opts, :ip)
    GenServer.cast(__MODULE__, {:untrack, user_id, ip})
  end

  @doc """
  Get connection count for a user.
  """
  def count(user_id) do
    case :ets.lookup(@table, user_id) do
      [{^user_id, count}] -> count
      [] -> 0
    end
  end

  @doc """
  Get connection count for an IP address.
  """
  def count_ip(ip) do
    case :ets.lookup(@ip_table, ip) do
      [{^ip, count}] -> count
      [] -> 0
    end
  end

  @doc """
  Get total global connection count (local instance).
  """
  def total_local do
    :ets.foldl(fn {_, count}, acc -> acc + count end, 0, @table)
  end

  @doc """
  Get total global connection count (all instances via Redis).
  Returns local count if Redis unavailable.
  """
  def total_global do
    case Redis.get("conn:global:total") do
      nil -> total_local()
      count when is_binary(count) -> String.to_integer(count)
      _ -> total_local()
    end
  end

  @doc """
  Get connection statistics.
  """
  def stats do
    total_users = :ets.info(@table, :size)
    total_connections = total_local()

    %{
      total_users: total_users,
      total_connections: total_connections,
      global_connections: total_global(),
      max_per_user: max_per_user(),
      max_per_ip: max_per_ip(),
      max_global: max_global()
    }
  end

  @doc """
  Get maximum connections per user (configurable).
  """
  def max_per_user do
    Application.get_env(:realtime, :max_connections_per_user, @default_max_per_user)
  end

  @doc """
  Get maximum connections per IP (configurable).
  """
  def max_per_ip do
    Application.get_env(:realtime, :max_connections_per_ip, @default_max_per_ip)
  end

  @doc """
  Get maximum global connections (configurable).
  """
  def max_global do
    Application.get_env(:realtime, :max_connections_global, @default_max_global)
  end

  # Server callbacks

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :set])
    :ets.new(@ip_table, [:named_table, :public, :set])

    Logger.info(
      "Connections tracker started (limits: user=#{max_per_user()}, ip=#{max_per_ip()}, global=#{max_global()})"
    )

    # State: %{monitor_ref => {user_id, ip}} so DOWN messages can find
    # the (user, ip) pair to decrement.
    {:ok, %{monitors: %{}}}
  end

  @impl true
  def handle_call({:track, user_id, ip, custom_max, pid}, _from, state) do
    case do_track(user_id, ip, custom_max) do
      :ok ->
        ref = Process.monitor(pid)
        monitors = Map.put(state.monitors, ref, {user_id, ip})
        {:reply, :ok, %{state | monitors: monitors}}

      error ->
        {:reply, error, state}
    end
  end

  @impl true
  def handle_cast({:untrack, user_id, ip}, state) do
    do_untrack(user_id, ip)
    {:noreply, state}
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _pid, _reason}, state) do
    case Map.pop(state.monitors, ref) do
      {nil, _} ->
        {:noreply, state}

      {{user_id, ip}, monitors} ->
        do_untrack(user_id, ip)
        {:noreply, %{state | monitors: monitors}}
    end
  end

  def handle_info(_msg, state) do
    {:noreply, state}
  end

  # Private functions

  defp do_track(user_id, ip, custom_max) do
    user_count = count(user_id)
    ip_count = if ip, do: count_ip(ip), else: 0
    global_count = total_local()

    # Use custom max if provided (subscription-based), otherwise use default
    user_max = custom_max || max_per_user()

    cond do
      user_count >= user_max ->
        Logger.debug("Connection rejected: user #{user_id} at limit (#{user_count}/#{user_max})")
        {:error, :limit_exceeded}

      ip && ip_count >= max_per_ip() ->
        Logger.debug("Connection rejected: IP #{ip} at limit (#{ip_count}/#{max_per_ip()})")
        {:error, :ip_limit_exceeded}

      global_count >= max_global() ->
        Logger.warning(
          "Connection rejected: global limit reached (#{global_count}/#{max_global()})"
        )

        {:error, :global_limit_exceeded}

      true ->
        # Track locally in ETS
        :ets.update_counter(@table, user_id, {2, 1}, {user_id, 0})

        if ip do
          :ets.update_counter(@ip_table, ip, {2, 1}, {ip, 0})
        end

        # Update Redis for distributed tracking (fire and forget)
        sync_to_redis(user_id, ip, :incr)

        :ok
    end
  end

  defp do_untrack(user_id, ip) do
    # Update user count
    case :ets.lookup(@table, user_id) do
      [{^user_id, count}] when count > 1 ->
        :ets.update_counter(@table, user_id, {2, -1})

      [{^user_id, _}] ->
        :ets.delete(@table, user_id)

      [] ->
        :ok
    end

    # Update IP count
    if ip do
      case :ets.lookup(@ip_table, ip) do
        [{^ip, count}] when count > 1 ->
          :ets.update_counter(@ip_table, ip, {2, -1})

        [{^ip, _}] ->
          :ets.delete(@ip_table, ip)

        [] ->
          :ok
      end
    end

    # Update Redis (fire and forget)
    sync_to_redis(user_id, ip, :decr)
  end

  defp sync_to_redis(user_id, ip, operation) do
    # Run async to not block the GenServer
    Task.start(fn ->
      commands =
        [
          redis_counter_command("conn:user:#{user_id}", operation),
          redis_counter_command("conn:global:total", operation)
        ] ++
          if ip do
            [redis_counter_command("conn:ip:#{ip}", operation)]
          else
            []
          end

      Redis.pipeline(commands)
    end)
  end

  defp redis_counter_command(key, :incr) do
    # INCR with expire
    [
      "EVAL",
      """
      local count = redis.call('INCR', KEYS[1])
      redis.call('EXPIRE', KEYS[1], ARGV[1])
      return count
      """,
      "1",
      key,
      to_string(@redis_ttl)
    ]
  end

  defp redis_counter_command(key, :decr) do
    # DECR with minimum of 0
    [
      "EVAL",
      """
      local count = redis.call('DECR', KEYS[1])
      if count < 0 then
        redis.call('SET', KEYS[1], 0)
        count = 0
      end
      redis.call('EXPIRE', KEYS[1], ARGV[1])
      return count
      """,
      "1",
      key,
      to_string(@redis_ttl)
    ]
  end
end
