defmodule Realtime.OriginCheck do
  @moduledoc """
  The websocket's Origin check: the configured dashboard origins (CHECK_ORIGIN_HOSTS),
  plus the https origin of any agency portal domain this instance has verified.

  Configured origins match the way Phoenix matches a `check_origin` list. A portal is
  looked up in `agency_brands` and cached briefly, so a domain that stops verifying
  stops being accepted within `@hit_ttl_ms`.
  """

  use GenServer

  @repo Application.compile_env(:realtime, :auth_repo, Realtime.Repo)
  @table :realtime_portal_origins
  @hit_ttl_ms 120_000
  @miss_ttl_ms 30_000
  @max_cached 8_192

  @query "SELECT 1 FROM agency_brands WHERE portal_domain = $1 AND portal_domain_verified LIMIT 1"

  # The process exists only to own the cache table.
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
    {:ok, nil}
  end

  @doc "The `check_origin` MFA target: true for a configured origin or a verified portal."
  def allowed?(%URI{} = uri, allowed) do
    configured?(uri, allowed) or portal?(uri)
  end

  @doc false
  def configured?(%URI{host: host} = uri, allowed) when is_binary(host) do
    host = String.downcase(host)

    Enum.any?(allowed, fn a ->
      same?(uri.scheme, a.scheme) and same?(uri.port, a.port) and host?(host, a.host)
    end)
  end

  def configured?(_uri, _allowed), do: false

  @doc "True for the plain https origin of a verified agency portal domain."
  def portal?(%URI{scheme: "https", host: host, port: 443, path: path, userinfo: nil})
      when is_binary(host) and path in [nil, ""] do
    host = String.downcase(host)

    if valid_host?(host), do: verified?(host), else: false
  end

  def portal?(_uri), do: false

  defp same?(_request, nil), do: true
  defp same?(request, allowed), do: request == allowed

  defp host?(_request, nil), do: true

  defp host?(request, "*." <> allowed),
    do: request == allowed or String.ends_with?(request, "." <> allowed)

  defp host?(request, allowed), do: request == allowed

  defp valid_host?(host) do
    byte_size(host) <= 253 and String.contains?(host, ".") and
      Regex.match?(
        ~r/\A[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+\z/,
        host
      )
  end

  defp verified?(host) do
    now = System.monotonic_time(:millisecond)

    case cached(host, now) do
      {:ok, answer} ->
        answer

      :miss ->
        case @repo.query(@query, [host]) do
          {:ok, %{rows: rows}} ->
            answer = rows != []
            remember(host, answer, now)
            answer

          # A failed read refuses and is not remembered.
          _ ->
            false
        end
    end
  end

  defp cached(host, now) do
    if table?() do
      case :ets.lookup(@table, host) do
        [{^host, answer, expires}] when expires > now -> {:ok, answer}
        _ -> :miss
      end
    else
      :miss
    end
  end

  defp remember(host, answer, now) do
    if table?() do
      if :ets.info(@table, :size) >= @max_cached, do: :ets.delete_all_objects(@table)
      ttl = if answer, do: @hit_ttl_ms, else: @miss_ttl_ms
      :ets.insert(@table, {host, answer, now + ttl})
    end

    :ok
  end

  defp table?, do: :ets.whereis(@table) != :undefined

  @doc false
  def clear, do: if(table?(), do: :ets.delete_all_objects(@table), else: true)
end
