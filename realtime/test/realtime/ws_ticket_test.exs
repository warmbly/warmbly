defmodule Realtime.WsTicketTest do
  @moduledoc """
  A socket ticket opens one socket, and only while the session it names is live.
  """

  use ExUnit.Case, async: false

  alias Realtime.Auth
  alias Realtime.Test.AuthRepo
  alias Realtime.Test.Counter

  @user "0b7d5c1e-6a1f-4c1e-9d5a-2f0c8e4b7a10"
  @session "1c8e6d2f-7b2a-4d2f-8e6b-3a1d9f5c8b21"

  setup do
    Counter.reset()
    start_supervised!(AuthRepo)
    :ok
  end

  defp ticket(extra \\ %{}) do
    claims =
      Map.merge(
        %{
          "sub" => @user,
          "sid" => UUID.uuid4(),
          "nonce" => "nonce",
          "purpose" => "ws",
          "exp" => System.system_time(:second) + 600
        },
        extra
      )

    JOSE.JWT.sign(JOSE.JWK.from_oct("test_jwt_secret"), %{"alg" => "HS256"}, claims)
    |> JOSE.JWS.compact()
    |> elem(1)
  end

  defp live_session(mfa_verified) do
    AuthRepo.respond_with(fn sql, _params ->
      if String.contains?(sql, "FROM sessions"),
        do: {:ok, %{rows: [[mfa_verified]]}},
        else: {:ok, %{rows: []}}
    end)
  end

  test "a ticket from a live session carries that session's second factor" do
    live_session(true)

    assert {:ok, @user, :jwt, %{session_id: @session, mfa_verified: true}} =
             Auth.verify_jwt(ticket(%{"session_id" => @session}))

    live_session(false)

    assert {:ok, @user, :jwt, %{mfa_verified: false}} =
             Auth.verify_jwt(ticket(%{"session_id" => @session}))
  end

  test "a ticket from a revoked or expired session is refused" do
    AuthRepo.respond_with(fn _sql, _params -> {:ok, %{rows: []}} end)

    assert {:error, :session_revoked} = Auth.verify_jwt(ticket(%{"session_id" => @session}))
  end

  test "a session lookup that fails refuses the ticket" do
    AuthRepo.respond_with(fn _sql, _params -> {:error, :timeout} end)

    assert {:error, :database_error} = Auth.verify_jwt(ticket(%{"session_id" => @session}))
  end

  test "a ticket opens one socket" do
    live_session(true)
    token = ticket(%{"session_id" => @session})

    assert {:ok, @user, :jwt, _} = Auth.verify_jwt(token)
    assert {:error, :ticket_used} = Auth.verify_jwt(token)
  end

  test "a ticket that names no session is refused" do
    assert {:error, :invalid_claims} = Auth.verify_jwt(ticket())
    assert {:error, :invalid_claims} = Auth.verify_jwt(ticket(%{"session_id" => ""}))
    assert AuthRepo.queries() == []
  end

  test "a ticket with no id is refused" do
    assert {:error, :invalid_claims} = Auth.verify_jwt(ticket(%{"sid" => nil}))
  end
end
