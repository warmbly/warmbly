defmodule Realtime.Auth do
  @moduledoc """
  Token verification for WebSocket connections.

  Supports:
  - JWT tokens issued by the Go backend
  - API keys prefixed with `wmbly_`
  - OAuth2 access tokens prefixed with `wmat_`
  """

  require Logger

  alias Realtime.ApiKey
  alias Realtime.ErrorReporter
  alias Realtime.OAuthToken

  @repo Application.compile_env(:realtime, :auth_repo, Realtime.Repo)

  @doc """
  Verifies a token (JWT or API key) and returns the user_id if valid.

  Detects token type by prefix:
  - `wmat_` prefix = OAuth2 access token
  - `wmbly_` prefix = API key
  - Otherwise = JWT token

  Returns:
  - {:ok, user_id, :jwt}, {:ok, user_id, :api_key} or {:ok, user_id, :oauth} on success
  - {:error, reason} on failure
  """
  def verify_token(token, opts \\ [])

  def verify_token(nil, _opts), do: {:error, :missing_token}
  def verify_token("", _opts), do: {:error, :missing_token}

  def verify_token(token, opts) do
    cond do
      OAuthToken.is_oauth_token?(token) ->
        verify_oauth_token(token, opts)

      ApiKey.is_api_key?(token) ->
        verify_api_key(token, opts)

      true ->
        verify_jwt(token)
    end
  end

  @doc """
  True for a credential that outlives a session (API key or OAuth token).
  """
  def long_lived?(token) when is_binary(token),
    do: OAuthToken.is_oauth_token?(token) or ApiKey.is_api_key?(token)

  def long_lived?(_), do: false

  @doc """
  Verify a JWT token.
  """
  def verify_jwt(token) do
    secret = Application.get_env(:realtime, :jwt_secret)

    case JOSE.JWT.verify_strict(jwk(secret), ["HS256"], token) do
      {true, %JOSE.JWT{fields: fields}, _} ->
        case validate_claims(fields) do
          {:ok, user_id} -> {:ok, user_id, :jwt}
          error -> error
        end

      {false, _, _} ->
        {:error, :invalid_signature}

      {:error, reason} ->
        Logger.warning("JWT verification failed: #{inspect(reason)}")
        {:error, :verification_failed}
    end
  rescue
    e ->
      Logger.error("JWT verification error: #{inspect(e)}")
      ErrorReporter.capture_exception(e, stacktrace: __STACKTRACE__)
      {:error, :verification_error}
  end

  # Only the backend's ticket-bound proof can replace the socket peer address.
  def proxy_ip(proof, ticket) when is_binary(proof) and is_binary(ticket) do
    secret = Application.get_env(:realtime, :jwt_secret)
    now = System.system_time(:second)

    with {true, %JOSE.JWT{fields: claims}, _} <-
           JOSE.JWT.verify_strict(jwk(secret), ["HS256"], proof),
         {true, %JOSE.JWT{fields: credential}, _} <-
           JOSE.JWT.verify_strict(jwk(secret), ["HS256"], ticket),
         {:ok, user_id} <- validate_claims(credential),
         %{
           "purpose" => "ws_proxy",
           "sub" => ^user_id,
           "exp" => exp,
           "iat" => iat,
           "sid" => sid,
           "nonce" => nonce,
           "client_ip" => ip
         } <- claims,
         true <-
           is_integer(exp) and is_integer(iat) and exp > now and iat <= now + 5 and
             exp - iat <= 30,
         true <-
           is_binary(sid) and is_binary(nonce) and sid == credential["sid"] and
             nonce == credential["nonce"],
         true <- is_binary(ip),
         {:ok, address} <- :inet.parse_address(String.to_charlist(ip)) do
      {:ok, address |> :inet.ntoa() |> to_string()}
    else
      _ -> :error
    end
  rescue
    _ -> :error
  end

  def proxy_ip(_, _), do: :error

  @doc """
  Verify an API key.
  """
  def verify_api_key(api_key, opts \\ []) do
    case ApiKey.validate(api_key, opts) do
      {:ok, user_id} ->
        {:ok, user_id, :api_key}

      {:error, reason} ->
        Logger.warning("API key verification failed: #{inspect(reason)}")
        {:error, reason}
    end
  end

  @doc """
  Verify an OAuth2 access token.
  """
  def verify_oauth_token(token, opts \\ []) do
    case OAuthToken.validate(token, opts) do
      {:ok, user_id} ->
        {:ok, user_id, :oauth}

      {:error, reason} ->
        Logger.warning("OAuth token verification failed: #{inspect(reason)}")
        {:error, reason}
    end
  end

  @doc """
  Map error reasons to Discord-style error codes.
  """
  def error_code(:missing_token), do: 4003
  def error_code(:credential_in_query), do: 4003
  def error_code(:invalid_signature), do: 4004
  def error_code(:verification_failed), do: 4004
  def error_code(:verification_error), do: 4004
  def error_code(:token_expired), do: 4004
  def error_code(:invalid_claims), do: 4004
  def error_code(:missing_subject), do: 4004
  def error_code(:wrong_token_purpose), do: 4004
  def error_code(:invalid_key), do: 4004
  def error_code(:key_inactive), do: 4004
  def error_code(:key_expired), do: 4004
  def error_code(:token_revoked), do: 4004
  def error_code(:database_error), do: 4004
  def error_code(:permission_denied), do: 4010
  def error_code(:ip_not_allowed), do: 4010
  def error_code(:not_a_member), do: 4010
  def error_code(:forbidden), do: 4010
  def error_code(:invalid_topic), do: 4005
  def error_code(:rate_limited), do: 4007
  def error_code(:limit_exceeded), do: 4009
  def error_code(_), do: 4004

  @doc """
  Map error reasons to human-readable messages.
  """
  def error_message(:missing_token), do: "Not authenticated"

  def error_message(:credential_in_query),
    do: "Send API keys and OAuth tokens in the x-warmbly-token header, not the URL"

  def error_message(:invalid_signature), do: "Authentication failed"
  def error_message(:verification_failed), do: "Authentication failed"
  def error_message(:verification_error), do: "Authentication failed"
  def error_message(:token_expired), do: "Token expired"
  def error_message(:invalid_claims), do: "Invalid token claims"
  def error_message(:missing_subject), do: "Invalid token claims"
  # Deliberately indistinguishable from any other authentication failure: a
  # caller presenting the wrong kind of token learns nothing about which
  # kinds exist.
  def error_message(:wrong_token_purpose), do: "Authentication failed"
  def error_message(:invalid_key), do: "Invalid API key"
  def error_message(:key_inactive), do: "API key inactive"
  def error_message(:key_expired), do: "API key expired"
  def error_message(:token_revoked), do: "Access token revoked"
  def error_message(:database_error), do: "Authentication failed"
  def error_message(:permission_denied), do: "Permission denied"
  def error_message(:ip_not_allowed), do: "IP address not allowed"
  def error_message(:invalid_topic), do: "Invalid channel topic"
  def error_message(:rate_limited), do: "Rate limited"
  def error_message(:limit_exceeded), do: "Connection limit exceeded"
  def error_message(:not_a_member), do: "Not a member of this organization"
  def error_message(:forbidden), do: "Access forbidden"
  def error_message(:not_found), do: "Resource not found"
  def error_message(_), do: "Authentication failed"

  @doc """
  Check if a user is a member of an organization.

  Returns:
  - {:ok, member} with member details including permissions
  - {:error, :not_a_member} if user is not a member
  """
  def check_org_membership(user_id, org_id) do
    query = """
    SELECT om.id, om.role, om.permissions,
           o.presence_show_online, o.presence_show_activity, om.access_scope
    FROM organization_members om
    JOIN organizations o ON o.id = om.organization_id
    WHERE om.organization_id = $1 AND om.user_id = $2
    """

    with {:ok, org_bin} <- dump_uuid(org_id),
         {:ok, user_bin} <- dump_uuid(user_id) do
      run_org_membership(query, org_bin, user_bin, org_id, user_id)
    else
      _ -> {:error, :not_a_member}
    end
  end

  defp run_org_membership(query, org_bin, user_bin, org_id, user_id) do
    case @repo.query(query, [org_bin, user_bin]) do
      {:ok, %{rows: [[id, role, permissions, show_online, show_activity, access_scope] | _]}} ->
        with {:ok, scope} <- member_scope(role, access_scope, org_bin, user_bin) do
          {:ok,
           %{
             id: id,
             role: role,
             permissions: permissions,
             organization_id: org_id,
             user_id: user_id,
             # Org-wide presence privacy. Default to visible if somehow null.
             presence_show_online: show_online != false,
             presence_show_activity: show_activity != false,
             # nil for the whole workspace; a restricted member's grants otherwise.
             scope: scope
           }}
        end

      {:ok, %{rows: []}} ->
        {:error, :not_a_member}

      {:error, _reason} ->
        {:error, :database_error}
    end
  end

  # The owner is never restricted, whatever the row says.
  defp member_scope("owner", _access_scope, _org_bin, _user_bin), do: {:ok, nil}
  defp member_scope(_role, "restricted", org_bin, user_bin), do: load_scope(org_bin, user_bin)
  defp member_scope(_role, _access_scope, _org_bin, _user_bin), do: {:ok, nil}

  # Mirrors ResolveMemberScope in the backend: folder grants follow the folder's current contents.
  defp load_scope(org_bin, user_bin) do
    query = """
    WITH granted_folders AS (
      SELECT f.id FROM folders f
      JOIN organization_member_campaign_access a ON a.folder_id = f.id
      WHERE f.organization_id = $1 AND a.organization_id = $1 AND a.user_id = $2
    ), in_scope AS (
      SELECT c.id FROM campaigns c
      WHERE c.organization_id = $1 AND (
        c.id IN (SELECT a.campaign_id FROM organization_member_campaign_access a
          WHERE a.organization_id = $1 AND a.user_id = $2 AND a.campaign_id IS NOT NULL)
        OR c.id IN (SELECT cf.campaign_id FROM campaign_folders cf WHERE cf.folder_id IN (SELECT id FROM granted_folders)))
    )
    SELECT
      COALESCE((SELECT array_agg(id::text) FROM in_scope), '{}'),
      COALESCE((SELECT array_agg(DISTINCT id::text) FROM (
        SELECT id FROM granted_folders
        UNION SELECT cf.folder_id FROM campaign_folders cf WHERE cf.campaign_id IN (SELECT id FROM in_scope)) v), '{}'),
      COALESCE((SELECT array_agg(ea.id::text) FROM email_accounts ea
        JOIN organization_member_mailbox_access a ON a.email_account_id = ea.id
        WHERE ea.organization_id = $1 AND a.organization_id = $1 AND a.user_id = $2), '{}')
    """

    case @repo.query(query, [org_bin, user_bin]) do
      {:ok, %{rows: [[campaigns, folders, mailboxes] | _]}} ->
        {:ok,
         %{
           campaigns: MapSet.new(campaigns),
           folders: MapSet.new(folders),
           mailboxes: MapSet.new(mailboxes)
         }}

      {:error, _reason} ->
        {:error, :database_error}
    end
  end

  @doc """
  Whether a membership resolved by check_org_membership reaches a resource.
  `kind` is :campaigns, :folders or :mailboxes; a workspace member reaches everything.
  """
  def in_scope?(%{scope: nil}, _kind, _id), do: true

  def in_scope?(%{scope: scope}, kind, id) when is_binary(id),
    do: MapSet.member?(Map.fetch!(scope, kind), String.downcase(id))

  def in_scope?(%{scope: _}, _kind, _id), do: false
  def in_scope?(_member, _kind, _id), do: true

  @doc false
  def matches_org?(_member, org_id) when org_id in [nil, ""], do: true

  def matches_org?(%{organization_id: member_org_id}, org_id) do
    with {:ok, id} <- dump_uuid(org_id),
         {:ok, ^id} <- dump_uuid(member_org_id) do
      true
    else
      _ -> false
    end
  end

  def matches_org?(_member, _org_id), do: false

  @doc """
  Fetch a user's display profile (name + avatar) for presence metadata.
  Best-effort: returns nil fields when the user can't be loaded, so a
  DB hiccup degrades presence labels rather than blocking the join.
  """
  def get_user_profile(user_id) do
    query = "SELECT first_name, last_name, avatar_url FROM users WHERE id = $1"

    with {:ok, uuid} <- Ecto.UUID.dump(user_id),
         {:ok, %{rows: [[first, last, avatar] | _]}} <- @repo.query(query, [uuid]) do
      %{name: String.trim("#{first} #{last}"), avatar: avatar}
    else
      _ -> %{name: nil, avatar: nil}
    end
  end

  @doc """
  Check if a user has access to a campaign via organization membership.

  Returns:
  - {:ok, member} with member details if user has access
  - {:error, :not_found} if campaign doesn't exist
  - {:error, :forbidden} if user doesn't have access
  """
  def check_campaign_access(user_id, campaign_id) do
    # First get the campaign's organization
    org_query = """
    SELECT c.organization_id
    FROM campaigns c
    WHERE c.id = $1
    """

    case dump_and_query(org_query, [campaign_id]) do
      {:ok, %{rows: [[org_id] | _]}} when not is_nil(org_id) ->
        # Check if user is a member of the organization, and reaches this campaign
        with {:ok, member} <- check_org_membership(user_id, org_id) do
          if in_scope?(member, :campaigns, campaign_id),
            do: {:ok, member},
            else: {:error, :forbidden}
        end

      {:ok, %{rows: [[nil] | _]}} ->
        # Campaign exists but has no organization - check direct ownership
        check_campaign_direct_access(user_id, campaign_id)

      {:ok, %{rows: []}} ->
        {:error, :not_found}

      {:error, _reason} ->
        {:error, :database_error}
    end
  end

  @doc """
  Check if a user has direct access to a campaign (legacy, pre-organization).
  """
  def check_campaign_direct_access(user_id, campaign_id) do
    query = """
    SELECT c.id, c.user_id
    FROM campaigns c
    WHERE c.id = $1 AND c.user_id = $2
    """

    case dump_and_query(query, [campaign_id, user_id]) do
      {:ok, %{rows: [_ | _]}} ->
        # Full permissions for direct owner
        {:ok, %{permissions: 65535}}

      {:ok, %{rows: []}} ->
        {:error, :forbidden}

      {:error, _reason} ->
        {:error, :database_error}
    end
  end

  @doc """
  Check if a user has access to an email account via organization membership.
  """
  def check_email_account_access(user_id, email_account_id) do
    org_query = """
    SELECT ea.organization_id
    FROM email_accounts ea
    WHERE ea.id = $1
    """

    case dump_and_query(org_query, [email_account_id]) do
      {:ok, %{rows: [[org_id] | _]}} when not is_nil(org_id) ->
        with {:ok, member} <- check_org_membership(user_id, org_id) do
          if in_scope?(member, :mailboxes, email_account_id),
            do: {:ok, member},
            else: {:error, :forbidden}
        end

      {:ok, %{rows: [[nil] | _]}} ->
        # Check direct ownership
        check_email_account_direct_access(user_id, email_account_id)

      {:ok, %{rows: []}} ->
        {:error, :not_found}

      {:error, _reason} ->
        {:error, :database_error}
    end
  end

  @doc """
  Check if a user has direct access to an email account (legacy).
  """
  def check_email_account_direct_access(user_id, email_account_id) do
    query = """
    SELECT ea.id
    FROM email_accounts ea
    WHERE ea.id = $1 AND ea.user_id = $2
    """

    case dump_and_query(query, [email_account_id, user_id]) do
      {:ok, %{rows: [_ | _]}} ->
        {:ok, %{permissions: 65535}}

      {:ok, %{rows: []}} ->
        {:error, :forbidden}

      {:error, _reason} ->
        {:error, :database_error}
    end
  end

  @doc false
  def check_user_event_resource(user_id, :notification, id) do
    query = "SELECT organization_id, category FROM notifications WHERE id = $1 AND user_id = $2"

    case dump_and_query(query, [id, user_id]) do
      {:ok, %{rows: [[org_id, _category]]}} when not is_nil(org_id) ->
        check_org_membership(user_id, org_id)

      {:ok, %{rows: [[nil, "security_new_signin"]]}} ->
        {:ok, %{permissions: 65535, scope: nil}}

      {:ok, %{rows: _}} ->
        {:error, :forbidden}

      {:error, _reason} ->
        {:error, :database_error}
    end
  end

  def check_user_event_resource(user_id, :contact, id) do
    query = """
    SELECT organization_id FROM contacts
    WHERE id = $1 AND (organization_id IS NOT NULL OR user_id = $2)
    """

    event_membership(query, [id, user_id], user_id)
  end

  def check_user_event_resource(user_id, :booking, id) do
    query = "SELECT organization_id, campaign_id FROM meeting_bookings WHERE id = $1"
    event_membership(query, [id], user_id)
  end

  defp event_membership(query, params, user_id) do
    case dump_and_query(query, params) do
      {:ok, %{rows: [[org_id | resource] | _]}} when not is_nil(org_id) ->
        with {:ok, member} <- check_org_membership(user_id, org_id) do
          {:ok, Map.put(member, :event_campaign_id, resource_id(resource))}
        end

      {:ok, %{rows: [[nil]]}} when length(params) == 2 ->
        {:ok, %{permissions: 65535, scope: nil}}

      {:ok, %{rows: _}} ->
        {:error, :forbidden}

      {:error, _reason} ->
        {:error, :database_error}
    end
  end

  defp resource_id([id]) when not is_nil(id), do: Ecto.UUID.load!(id)
  defp resource_id(_), do: nil

  @doc """
  Check if a user is a platform admin (users.admin_permissions > 0).

  Gate for the internal `admin:platform` channel; returns the admin
  permission bitmap so future per-permission event filtering can use it.
  """
  def check_admin(user_id) do
    query = """
    SELECT u.admin_permissions
    FROM users u
    WHERE u.id = $1 AND u.admin_permissions > 0
    """

    case dump_and_query(query, [user_id]) do
      {:ok, %{rows: [[permissions] | _]}} ->
        {:ok, %{permissions: permissions}}

      {:ok, %{rows: []}} ->
        {:error, :not_an_admin}

      {:error, _reason} ->
        {:error, :database_error}
    end
  end

  # Postgrex encodes uuid params as 16-byte binaries; accept both the raw
  # binary (from a prior query's row) and the canonical string form.
  defp dump_uuid(<<_::128>> = bin), do: {:ok, bin}
  defp dump_uuid(value) when is_binary(value), do: Ecto.UUID.dump(value)
  defp dump_uuid(_), do: :error

  # Run a query whose params are all uuids, dumping each first. An
  # undumpable value behaves like an empty result, not a crash.
  defp dump_and_query(query, params) do
    dumped =
      Enum.reduce_while(params, {:ok, []}, fn value, {:ok, acc} ->
        case dump_uuid(value) do
          {:ok, bin} -> {:cont, {:ok, [bin | acc]}}
          _ -> {:halt, :error}
        end
      end)

    case dumped do
      {:ok, bins} -> @repo.query(query, Enum.reverse(bins))
      :error -> {:ok, %{rows: []}}
    end
  end

  @doc """
  Check if member has a specific permission.
  Permission is a bitmask value.
  """
  def has_permission?(member, permission) when is_map(member) do
    permissions = Map.get(member, :permissions, 0)
    Bitwise.band(permissions, permission) == permission
  end

  # Permission constants matching Go models
  @perm_manage_team 1
  @perm_manage_billing 2
  @perm_manage_campaigns 4
  @perm_manage_contacts 8
  @perm_manage_emails 16
  @perm_view_analytics 32
  @perm_send_campaigns 64
  @perm_access_unibox 128
  @perm_manage_sequences 256
  @perm_manage_settings 512
  @perm_view_campaigns 1024
  @perm_view_contacts 2048
  @perm_transfer_ownership 4096

  def permission(:manage_team), do: @perm_manage_team
  def permission(:manage_billing), do: @perm_manage_billing
  def permission(:manage_campaigns), do: @perm_manage_campaigns
  def permission(:manage_contacts), do: @perm_manage_contacts
  def permission(:manage_emails), do: @perm_manage_emails
  def permission(:view_analytics), do: @perm_view_analytics
  def permission(:send_campaigns), do: @perm_send_campaigns
  def permission(:access_unibox), do: @perm_access_unibox
  def permission(:manage_sequences), do: @perm_manage_sequences
  def permission(:manage_settings), do: @perm_manage_settings
  def permission(:view_campaigns), do: @perm_view_campaigns
  def permission(:view_contacts), do: @perm_view_contacts
  def permission(:transfer_ownership), do: @perm_transfer_ownership

  # Private functions

  defp jwk(secret) do
    JOSE.JWK.from_oct(secret)
  end

  # The backend signs every one of its tokens with this same key: the session
  # access token, the refresh token, the challenge token issued after a password
  # but before the emailed code, the 2FA pending token, and the password-reset
  # link token. They share a claim shape, so checking only `sub` and `exp`
  # accepted all of them here. The backend is safe because each is separately
  # bound to a Redis nonce or a `sessions` row; this service checks neither.
  #
  # So the socket takes exactly one kind: the short-lived ticket minted by
  # POST /v1/getaway for this purpose. Anything else is refused.
  @ws_purpose "ws"

  defp validate_claims(%{"sub" => user_id, "exp" => exp} = claims) do
    now = System.system_time(:second)

    cond do
      is_nil(user_id) or user_id == "" ->
        {:error, :missing_subject}

      exp < now ->
        {:error, :token_expired}

      Map.get(claims, "purpose") != @ws_purpose ->
        {:error, :wrong_token_purpose}

      true ->
        {:ok, user_id}
    end
  end

  defp validate_claims(%{"user_id" => user_id, "exp" => exp} = claims) do
    validate_claims(
      claims
      |> Map.put("sub", user_id)
      |> Map.put("exp", exp)
    )
  end

  defp validate_claims(_) do
    {:error, :invalid_claims}
  end
end
