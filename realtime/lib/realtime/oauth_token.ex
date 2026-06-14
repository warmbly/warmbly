defmodule Realtime.OAuthToken do
  @moduledoc """
  OAuth2 access token validation and authentication.

  Validates access tokens prefixed with `wmat_` by:
  1. Hashing with SHA-256 (same scheme as `Realtime.ApiKey`)
  2. Looking up in the `oauth_access_grants` table
  3. Checking the token is neither revoked nor expired
  4. Checking the `REALTIME_SUBSCRIBE` scope (bit 11) is granted

  Tokens carry their permissions in the `scopes` bigint, which uses the same
  api_permission bitmask as API keys.
  """

  require Logger

  alias Realtime.ApiKey
  alias Realtime.ErrorReporter
  alias Realtime.Repo

  import Ecto.Query

  # OAuth2 access token prefix
  @prefix "wmat_"

  # Permission bit for realtime subscription (bit 11 = value 2048)
  @perm_realtime_subscribe 11

  @doc """
  Check if a token is an OAuth2 access token (starts with `wmat_` prefix).
  """
  def is_oauth_token?(nil), do: false
  def is_oauth_token?(""), do: false

  def is_oauth_token?(token) when is_binary(token) do
    String.starts_with?(token, @prefix)
  end

  @doc """
  Validate an OAuth2 access token and return the user_id if valid.

  Returns:
  - {:ok, user_id} if valid
  - {:error, reason} if invalid

  Checks:
  - Token exists
  - Token is not revoked
  - Token is not expired
  - Token has realtime subscription scope (bit 11)
  """
  def validate(token, _opts \\ []) do
    with {:ok, grant} <- lookup_token(token),
         :ok <- check_revoked(grant),
         :ok <- check_expiration(grant),
         :ok <- check_scope(grant) do
      {:ok, grant.user_id}
    end
  end

  # Reuse the API key hashing so both token types hash identically.
  defp hash_token(token), do: ApiKey.hash_key(token)

  defp lookup_token(token) do
    token_hash = hash_token(token)

    query =
      from(g in "oauth_access_grants",
        where: g.access_token_hash == ^token_hash,
        select: %{
          user_id: g.user_id,
          scopes: g.scopes,
          access_expires_at: g.access_expires_at,
          revoked_at: g.revoked_at
        }
      )

    case Repo.one(query) do
      nil ->
        {:error, :invalid_key}

      grant ->
        {:ok, grant}
    end
  rescue
    e ->
      Logger.error("OAuth token query failed: #{inspect(e)}")
      ErrorReporter.capture_exception(e)
      {:error, :database_error}
  end

  defp check_revoked(%{revoked_at: nil}), do: :ok

  defp check_revoked(%{revoked_at: _revoked_at}) do
    {:error, :token_revoked}
  end

  defp check_expiration(%{access_expires_at: nil}), do: :ok

  defp check_expiration(%{access_expires_at: expires_at}) when is_struct(expires_at, DateTime) do
    if DateTime.compare(DateTime.utc_now(), expires_at) == :lt do
      :ok
    else
      {:error, :key_expired}
    end
  end

  defp check_expiration(%{access_expires_at: expires_at})
       when is_struct(expires_at, NaiveDateTime) do
    if NaiveDateTime.compare(NaiveDateTime.utc_now(), expires_at) == :lt do
      :ok
    else
      {:error, :key_expired}
    end
  end

  defp check_expiration(_), do: :ok

  defp check_scope(%{scopes: scopes}) when is_integer(scopes) do
    if has_permission?(scopes, @perm_realtime_subscribe) do
      :ok
    else
      {:error, :permission_denied}
    end
  end

  defp check_scope(_) do
    # No scopes, deny by default
    {:error, :permission_denied}
  end

  defp has_permission?(scopes, bit) do
    Bitwise.band(scopes, Bitwise.bsl(1, bit)) != 0
  end
end
