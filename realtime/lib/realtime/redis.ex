defmodule Realtime.Redis do
  @moduledoc """
  Redis connection pool for rate limiting and distributed state.

  Implements fail-open behavior - if Redis is unavailable,
  operations return safe defaults rather than failing.
  """

  use Supervisor

  require Logger

  @pool_size 10
  @default_timeout 5_000

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    redis_url = Application.get_env(:realtime, :redis_url, "redis://localhost:6379/0")
    opts = tls_opts(redis_url)

    children =
      for i <- 0..(@pool_size - 1) do
        Supervisor.child_spec(
          {Redix, {redis_url, [name: :"redix_#{i}"] ++ opts}},
          id: {Redix, i}
        )
      end

    Supervisor.init(children, strategy: :one_for_one)
  end

  @doc false
  # Erlang's default hostname check does not match a wildcard certificate, and
  # every managed Redis presents one (`*.upstash.io`, ElastiCache, Redis Cloud).
  # Without the https match fun the handshake fails with
  # {:bad_cert, :hostname_check_failed} on all #{@pool_size} connections and the
  # pool never comes up, which reads as "Redis unavailable" and fails open.
  #
  # Redix drops only the keys given here from its own defaults, so verify_peer
  # and the system CA store still apply.
  def tls_opts(url) do
    if String.starts_with?(url, "rediss://") do
      [
        socket_opts: [
          customize_hostname_check: [
            match_fun: :public_key.pkix_verify_hostname_match_fun(:https)
          ]
        ]
      ]
    else
      []
    end
  end

  @doc """
  Execute a Redis command. Returns {:ok, result} or {:error, reason}.
  Fails open with a default value if Redis is unavailable.
  """
  def command(args, opts \\ []) do
    timeout = Keyword.get(opts, :timeout, @default_timeout)

    try do
      Redix.command(random_connection(), args, timeout: timeout)
    rescue
      e ->
        Logger.warning("Redis command failed: #{inspect(e)}")
        {:error, :redis_unavailable}
    catch
      :exit, reason ->
        Logger.warning("Redis command exit: #{inspect(reason)}")
        {:error, :redis_unavailable}
    end
  end

  @doc """
  Execute a Redis command, returning result directly or nil on failure.
  """
  def command!(args, opts \\ []) do
    case command(args, opts) do
      {:ok, result} -> result
      {:error, _} -> nil
    end
  end

  @doc """
  Execute a pipeline of Redis commands.
  """
  def pipeline(commands, opts \\ []) do
    timeout = Keyword.get(opts, :timeout, @default_timeout)

    try do
      Redix.pipeline(random_connection(), commands, timeout: timeout)
    rescue
      e ->
        Logger.warning("Redis pipeline failed: #{inspect(e)}")
        {:error, :redis_unavailable}
    catch
      :exit, reason ->
        Logger.warning("Redis pipeline exit: #{inspect(reason)}")
        {:error, :redis_unavailable}
    end
  end

  @doc """
  Increment a counter with TTL. Used for rate limiting.
  Returns the new count or nil on failure.
  """
  def incr_with_ttl(key, ttl_seconds) do
    case pipeline([
           ["INCR", key],
           ["EXPIRE", key, ttl_seconds]
         ]) do
      {:ok, [count, _]} -> count
      _ -> nil
    end
  end

  @doc """
  Get a value from Redis.
  """
  def get(key) do
    case command(["GET", key]) do
      {:ok, value} -> value
      {:error, _} -> nil
    end
  end

  @doc """
  Set a value in Redis with optional TTL.
  """
  def set(key, value, opts \\ []) do
    ttl = Keyword.get(opts, :ttl)

    args =
      if ttl do
        ["SET", key, value, "EX", ttl]
      else
        ["SET", key, value]
      end

    case command(args) do
      {:ok, "OK"} -> :ok
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  @doc """
  Delete a key from Redis.
  """
  def delete(key) do
    case command(["DEL", key]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  @doc """
  Check if Redis is available.
  """
  def available? do
    case command(["PING"]) do
      {:ok, "PONG"} -> true
      _ -> false
    end
  end

  # Private functions

  defp random_connection do
    :"redix_#{:rand.uniform(@pool_size) - 1}"
  end
end
