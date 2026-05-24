defmodule Realtime.ApiKey do
  @moduledoc """
  API key validation and authentication.

  Validates API keys prefixed with `wmbly_` by:
  1. Hashing with SHA-256
  2. Looking up in database (with Redis caching)
  3. Checking permissions (bit 11 for RealtimeSubscribe)
  4. Verifying expiration and status
  """

  require Logger

  alias Realtime.Redis
  alias Realtime.ErrorReporter
  alias Realtime.Repo

  import Ecto.Query

  # API key prefix
  @prefix "wmbly_"

  # Permission bit for realtime subscription
  @perm_realtime_subscribe 11

  # Cache TTL in seconds (5 minutes)
  @cache_ttl 300

  @doc """
  Check if a token is an API key (starts with wmbly_ prefix).
  """
  def is_api_key?(nil), do: false
  def is_api_key?(""), do: false

  def is_api_key?(token) when is_binary(token) do
    String.starts_with?(token, @prefix)
  end

  @doc """
  Validate an API key and return the user_id if valid.

  Returns:
  - {:ok, user_id} if valid
  - {:error, reason} if invalid

  Checks:
  - Key exists and is active
  - Key is not expired
  - Key has realtime subscription permission (bit 11)
  - IP is allowed (if IP restrictions configured)
  """
  def validate(api_key, opts \\ []) do
    client_ip = Keyword.get(opts, :ip)

    with {:ok, key_data} <- lookup_key(api_key),
         :ok <- check_status(key_data),
         :ok <- check_expiration(key_data),
         :ok <- check_permission(key_data),
         :ok <- check_ip_restriction(key_data, client_ip) do
      {:ok, key_data.user_id}
    end
  end

  @doc """
  Hash an API key using SHA-256.
  """
  def hash_key(api_key) do
    :crypto.hash(:sha256, api_key)
    |> Base.encode16(case: :lower)
  end

  # Private functions

  defp lookup_key(api_key) do
    key_hash = hash_key(api_key)
    cache_key = "apikey:#{key_hash}"

    # Try Redis cache first
    case get_cached(cache_key) do
      {:ok, key_data} ->
        {:ok, key_data}

      :miss ->
        # Query database
        case query_database(key_hash) do
          {:ok, key_data} ->
            cache_key_data(cache_key, key_data)
            {:ok, key_data}

          error ->
            error
        end
    end
  end

  defp get_cached(cache_key) do
    case Redis.get(cache_key) do
      nil ->
        :miss

      data when is_binary(data) ->
        case Jason.decode(data, keys: :atoms) do
          {:ok, key_data} -> {:ok, struct_from_map(key_data)}
          _ -> :miss
        end

      _ ->
        :miss
    end
  end

  defp cache_key_data(cache_key, key_data) do
    case Jason.encode(key_data) do
      {:ok, json} ->
        Redis.set(cache_key, json, ttl: @cache_ttl)

      _ ->
        :ok
    end
  end

  defp query_database(key_hash) do
    query =
      from(ak in "api_keys",
        where: ak.key_hash == ^key_hash,
        select: %{
          id: ak.id,
          user_id: ak.user_id,
          status: ak.status,
          permissions: ak.permissions,
          allowed_ips: ak.allowed_ips,
          expires_at: ak.expires_at
        }
      )

    case Repo.one(query) do
      nil ->
        {:error, :invalid_key}

      key_data ->
        {:ok, key_data}
    end
  rescue
    e ->
      Logger.error("Database query failed: #{inspect(e)}")
      ErrorReporter.capture_exception(e)
      {:error, :database_error}
  end

  defp struct_from_map(map) when is_map(map) do
    # Convert expires_at string back to datetime if present
    map =
      case Map.get(map, :expires_at) do
        nil ->
          map

        expires_at when is_binary(expires_at) ->
          case DateTime.from_iso8601(expires_at) do
            {:ok, dt, _} -> Map.put(map, :expires_at, dt)
            _ -> map
          end

        _ ->
          map
      end

    map
  end

  defp check_status(%{status: "active"}), do: :ok

  defp check_status(%{status: status}) do
    Logger.debug("API key status check failed: #{status}")
    {:error, :key_inactive}
  end

  defp check_expiration(%{expires_at: nil}), do: :ok

  defp check_expiration(%{expires_at: expires_at}) when is_struct(expires_at, DateTime) do
    if DateTime.compare(DateTime.utc_now(), expires_at) == :lt do
      :ok
    else
      {:error, :key_expired}
    end
  end

  defp check_expiration(%{expires_at: expires_at}) when is_binary(expires_at) do
    case DateTime.from_iso8601(expires_at) do
      {:ok, dt, _} ->
        check_expiration(%{expires_at: dt})

      _ ->
        # Can't parse expiration, assume valid
        :ok
    end
  end

  defp check_expiration(_), do: :ok

  defp check_permission(%{permissions: permissions}) when is_integer(permissions) do
    if has_permission?(permissions, @perm_realtime_subscribe) do
      :ok
    else
      {:error, :permission_denied}
    end
  end

  defp check_permission(_) do
    # No permissions field, deny by default
    {:error, :permission_denied}
  end

  defp has_permission?(permissions, bit) do
    Bitwise.band(permissions, Bitwise.bsl(1, bit)) != 0
  end

  defp check_ip_restriction(%{allowed_ips: nil}, _client_ip), do: :ok
  defp check_ip_restriction(%{allowed_ips: []}, _client_ip), do: :ok
  defp check_ip_restriction(_, nil), do: :ok

  defp check_ip_restriction(%{allowed_ips: allowed_ips}, client_ip) when is_list(allowed_ips) do
    if client_ip in allowed_ips do
      :ok
    else
      Logger.debug(
        "API key IP restriction check failed: #{client_ip} not in #{inspect(allowed_ips)}"
      )

      {:error, :ip_not_allowed}
    end
  end

  defp check_ip_restriction(_, _), do: :ok
end
