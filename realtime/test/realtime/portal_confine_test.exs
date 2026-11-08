defmodule Realtime.PortalConfineTest do
  @moduledoc """
  A session signed in on an agency portal joins only that agency's client workspaces.
  """

  use RealtimeWeb.ChannelCase, async: false

  alias Realtime.Auth
  alias Realtime.Test.AuthRepo
  alias RealtimeWeb.AdminChannel
  alias RealtimeWeb.BulkChannel
  alias RealtimeWeb.OrgChannel

  @client "0b7d5c1e-6a1f-4c1e-9d5a-2f0c8e4b7a10"
  @other_org "1c8e6d2f-7b2a-4d2f-8e6b-3a1d9f5c8b21"
  @agency "2d9f7e3a-8c3b-4e3a-9f7c-4b2e0a6d9c32"

  setup do
    start_supervised!(AuthRepo)

    AuthRepo.respond_with(fn sql, [org | _] ->
      cond do
        String.contains?(sql, "agency_clients") ->
          if org == Ecto.UUID.dump!(@client), do: {:ok, %{rows: [[1]]}}, else: {:ok, %{rows: []}}

        String.contains?(sql, "organization_members") ->
          {:ok, %{rows: [[UUID.uuid4(), "admin", 8191, true, true, nil]]}}

        true ->
          {:ok, %{rows: []}}
      end
    end)

    :ok
  end

  defp member(org), do: %{organization_id: org, permissions: 8191, scope: nil}

  defp portal_socket(user_id \\ UUID.uuid4()),
    do: user_socket(user_id, %{portal_agency_id: @agency, mfa_verified: true})

  test "a portal session keeps a membership in a client workspace and loses any other" do
    assigns = %{portal_agency_id: @agency}

    assert {:ok, _} = Auth.confine_socket({:ok, member(@client)}, assigns)

    assert {:error, :portal_session_restricted} =
             Auth.confine_socket({:ok, member(@other_org)}, assigns)

    assert {:error, :portal_session_restricted} =
             Auth.confine_socket({:ok, %{permissions: 65535}}, assigns)
  end

  test "a session not signed in on a portal is unchanged" do
    assert Auth.confine_socket({:ok, member(@other_org)}, %{}) == {:ok, member(@other_org)}
  end

  test "a portal session joins a client's org channel and is refused any other" do
    assert {:ok, _, _} = subscribe_and_join(portal_socket(), OrgChannel, "org:#{@client}")

    assert {:error, %{code: 4010, reason: "portal_session_restricted"}} =
             subscribe_and_join(portal_socket(), OrgChannel, "org:#{@other_org}")
  end

  test "a portal session never joins the admin channel" do
    assert {:error, %{reason: "portal_session_restricted"}} =
             subscribe_and_join(portal_socket(), AdminChannel, "admin:platform")
  end

  test "a portal session receives its own bulk operations only in client workspaces" do
    user_id = UUID.uuid4()
    socket = portal_socket(user_id)

    event = fn org ->
      %{"event_type" => "BULK_PROGRESS", "user_id" => user_id, "org_id" => org}
    end

    assert BulkChannel.own_event?(event.(@client), socket)
    refute BulkChannel.own_event?(event.(@other_org), socket)
    refute BulkChannel.own_event?(Map.delete(event.(@client), "org_id"), socket)
  end

  defp session_bound_to(agency) do
    AuthRepo.respond_with(fn sql, _params ->
      if String.contains?(sql, "FROM sessions"),
        do: {:ok, %{rows: [[false, agency]]}},
        else: {:ok, %{rows: []}}
    end)
  end

  defp ticket(purpose) do
    JOSE.JWT.sign(JOSE.JWK.from_oct("test_jwt_secret"), %{"alg" => "HS256"}, %{
      "sub" => UUID.uuid4(),
      "sid" => UUID.uuid4(),
      "nonce" => "n",
      "purpose" => purpose,
      "session_id" => UUID.uuid4(),
      "exp" => System.system_time(:second) + 600
    })
    |> JOSE.JWS.compact()
    |> elem(1)
  end

  test "a portal ticket carries the session's portal binding" do
    session_bound_to(@agency)
    assert {:ok, _, :jwt, %{portal_agency_id: @agency}} = Auth.verify_jwt(ticket("portal_ws"))
  end

  test "a ticket's purpose has to match whether its session is bound to a portal" do
    session_bound_to(@agency)
    assert {:error, :wrong_token_purpose} = Auth.verify_jwt(ticket("ws"))

    session_bound_to(nil)
    assert {:error, :wrong_token_purpose} = Auth.verify_jwt(ticket("portal_ws"))
    assert {:ok, _, :jwt, %{portal_agency_id: nil}} = Auth.verify_jwt(ticket("ws"))
  end
end
