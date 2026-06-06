defmodule Realtime.Subscription do
  @moduledoc """
  Fetches subscription and rate limit information for users.

  Supports:
  - Plan-based limits from subscription tier
  - Enterprise custom limits from user_rate_limits table
  - Caching via Redis to reduce database load
  """

  require Logger

  alias Realtime.Repo
  alias Realtime.Redis
  alias Realtime.ErrorReporter

  import Ecto.Query

  # Cache TTL in seconds (5 minutes)
  @cache_ttl 300

  # Default limits when no subscription exists
  @default_limits %{
    limit_ws_message_pm: 120,
    limit_ws_join_pm: 30,
    limit_ws_event_pm: 60,
    max_connections: 10
  }

  @doc """
  Get rate limits for a user based on their subscription.

  Returns limits from:
  1. user_rate_limits if enterprise subscription (custom limits)
  2. plan_rate_limits based on subscription plan
  3. Default limits if no subscription

  Results are cached in Redis for performance.
  """
  def get_limits(user_id) when is_binary(user_id) do
    cache_key = "sub:limits:#{user_id}"

    case get_cached_limits(cache_key) do
      {:ok, limits} ->
        limits

      :miss ->
        limits = fetch_limits_from_db(user_id)
        cache_limits(cache_key, limits)
        limits
    end
  end

  def get_limits(_), do: @default_limits

  @doc """
  Get subscription status for a user.

  Returns:
  - {:ok, status} where status is :active, :trialing, :past_due, etc.
  - {:error, :no_subscription} if user has no subscription
  """
  def get_status(user_id) when is_binary(user_id) do
    query =
      from(s in "subscriptions",
        where: s.user_id == type(^user_id, :binary_id),
        select: s.status
      )

    case Repo.one(query) do
      nil -> {:error, :no_subscription}
      status -> {:ok, String.to_atom(status)}
    end
  rescue
    e ->
      Logger.error("Failed to get subscription status: #{inspect(e)}")
      {:error, :database_error}
  end

  @doc """
  Check if user has an active subscription.
  """
  def is_active?(user_id) when is_binary(user_id) do
    case get_status(user_id) do
      {:ok, :active} -> true
      {:ok, :trialing} -> true
      _ -> false
    end
  end

  def is_active?(_), do: false

  @doc """
  Invalidate cached limits for a user.
  Called when subscription changes.
  """
  def invalidate_cache(user_id) do
    cache_key = "sub:limits:#{user_id}"
    Redis.delete(cache_key)
  end

  @doc """
  Get default limits.
  """
  def default_limits, do: @default_limits

  # Private functions

  defp fetch_limits_from_db(user_id) do
    # Query that handles enterprise custom limits vs plan-based limits
    query = """
    SELECT
      COALESCE(url.limit_ws_message_pm, prl.limit_ws_message_pm, 120) as limit_ws_message_pm,
      COALESCE(url.limit_ws_join_pm, prl.limit_ws_join_pm, 30) as limit_ws_join_pm,
      COALESCE(url.limit_ws_event_pm, prl.limit_ws_event_pm, 60) as limit_ws_event_pm,
      COALESCE(url.max_connections, prl.max_connections, 10) as max_connections
    FROM subscriptions s
    LEFT JOIN plan_rate_limits prl ON prl.plan_id = s.plan_id
    LEFT JOIN user_rate_limits url ON url.user_id = s.user_id AND s.is_enterprise = true
    WHERE s.user_id = $1
    """

    case Repo.query(query, [Ecto.UUID.dump!(user_id)]) do
      {:ok, %{rows: [[ws_message, ws_join, ws_event, max_conn]]}} ->
        %{
          limit_ws_message_pm: ws_message || 120,
          limit_ws_join_pm: ws_join || 30,
          limit_ws_event_pm: ws_event || 60,
          max_connections: max_conn || 10
        }

      {:ok, %{rows: []}} ->
        Logger.debug("No subscription found for user #{user_id}, using defaults")
        @default_limits

      {:error, error} ->
        Logger.error("Failed to fetch limits: #{inspect(error)}")
        @default_limits
    end
  rescue
    e ->
      Logger.error("Database error fetching limits: #{inspect(e)}")
      ErrorReporter.capture_exception(e)
      @default_limits
  end

  defp get_cached_limits(cache_key) do
    case Redis.get(cache_key) do
      nil ->
        :miss

      data when is_binary(data) ->
        case Jason.decode(data, keys: :atoms) do
          {:ok, limits} -> {:ok, limits}
          _ -> :miss
        end

      _ ->
        :miss
    end
  end

  defp cache_limits(cache_key, limits) do
    case Jason.encode(limits) do
      {:ok, json} ->
        Redis.set(cache_key, json, ttl: @cache_ttl)

      _ ->
        :ok
    end
  end
end
