defmodule Realtime.ProxyIPTest do
  use ExUnit.Case, async: true
  alias Realtime.Auth
  alias RealtimeWeb.UserSocket

  defp sign(claims, secret \\ "test_jwt_secret") do
    JOSE.JWT.sign(JOSE.JWK.from_oct(secret), %{"alg" => "HS256"}, claims)
    |> JOSE.JWS.compact()
    |> elem(1)
  end

  test "only a signed proof bound to the authenticated ticket can preserve the client IP" do
    now = System.system_time(:second)

    claims = %{
      "sub" => "user",
      "sid" => "ticket",
      "nonce" => "nonce",
      "purpose" => "ws",
      "exp" => now + 600
    }

    ticket = sign(claims)

    proof =
      Map.merge(claims, %{
        "purpose" => "ws_proxy",
        "iat" => now,
        "exp" => now + 30,
        "client_ip" => "192.0.2.10"
      })

    assert Auth.proxy_ip(sign(proof), ticket) == {:ok, "192.0.2.10"}

    assert UserSocket.client_ip(ticket, %{
             peer_data: %{address: {127, 0, 0, 1}},
             x_headers: [{"x-warmbly-proxy-proof", sign(proof)}]
           }) == "192.0.2.10"

    assert {:error, :wrong_token_purpose} = Auth.verify_jwt(sign(proof))

    for invalid <- [
          Map.put(proof, "sid", "different"),
          Map.put(proof, "nonce", "different"),
          Map.put(proof, "sub", "another-user"),
          Map.put(proof, "exp", now - 1),
          Map.put(proof, "client_ip", "not-an-ip"),
          Map.put(proof, "purpose", "ws")
        ] do
      assert Auth.proxy_ip(sign(invalid), ticket) == :error
    end

    assert Auth.proxy_ip(sign(proof, "wrong-key"), ticket) == :error
    assert Auth.proxy_ip(sign(proof), "wmbly_key") == :error

    assert UserSocket.client_ip(ticket, %{
             peer_data: %{address: {127, 0, 0, 1}},
             x_headers: [{"x-warmbly-proxy-proof", "forged"}, {"x-forwarded-for", "192.0.2.99"}]
           }) == "127.0.0.1"
  end
end
