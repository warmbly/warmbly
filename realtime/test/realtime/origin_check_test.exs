defmodule Realtime.OriginCheckTest do
  use ExUnit.Case, async: false

  alias Realtime.OriginCheck
  alias Realtime.Test.AuthRepo

  @dashboard [%{scheme: "https", host: "app.example.com", port: 443}]

  setup do
    start_supervised!(AuthRepo)
    OriginCheck.clear()

    AuthRepo.respond_with(fn _sql, [host] ->
      if host == "portal.agency.test", do: {:ok, %{rows: [[1]]}}, else: {:ok, %{rows: []}}
    end)

    :ok
  end

  defp origin(value), do: URI.parse(value)

  test "the configured dashboard origin is accepted as before" do
    assert OriginCheck.allowed?(origin("https://app.example.com"), @dashboard)
    refute OriginCheck.configured?(origin("http://app.example.com"), @dashboard)
    refute OriginCheck.configured?(origin("https://app.example.com:8443"), @dashboard)

    assert OriginCheck.configured?(origin("https://a.example.com"), [
             %{scheme: nil, host: "*.example.com", port: nil}
           ])
  end

  test "a verified portal domain is accepted and nothing else is" do
    assert OriginCheck.allowed?(origin("https://portal.agency.test"), @dashboard)
    refute OriginCheck.allowed?(origin("https://evil.test"), @dashboard)
    refute OriginCheck.allowed?(origin("http://portal.agency.test"), @dashboard)
    refute OriginCheck.allowed?(origin("https://portal.agency.test:8443"), @dashboard)
    refute OriginCheck.allowed?(origin("https://user@portal.agency.test"), @dashboard)
    refute OriginCheck.allowed?(origin("https://localhost"), @dashboard)
    refute OriginCheck.allowed?(origin("null"), @dashboard)
  end

  test "answers are cached and a failed read refuses without being remembered" do
    assert OriginCheck.portal?(origin("https://portal.agency.test"))
    assert OriginCheck.portal?(origin("https://portal.agency.test"))
    assert length(AuthRepo.queries()) == 1

    OriginCheck.clear()
    AuthRepo.respond_with(fn _sql, _params -> {:error, :database_unavailable} end)
    refute OriginCheck.portal?(origin("https://portal.agency.test"))

    AuthRepo.respond_with(fn _sql, _params -> {:ok, %{rows: [[1]]}} end)
    assert OriginCheck.portal?(origin("https://portal.agency.test"))
  end
end
