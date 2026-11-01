defmodule Realtime.ServeTLSTest do
  use ExUnit.Case, async: false

  alias Realtime.ServeTLS

  defp env(pairs), do: fn name -> Map.get(Map.new(pairs), name) end

  test "unset leaves the endpoint untouched" do
    assert ServeTLS.endpoint_overrides(env([])) == {:ok, []}
    assert ServeTLS.endpoint_overrides(env([{"SERVE_TLS", "off"}])) == {:ok, []}
  end

  test "self-signed adds a TLS 1.2+ listener with an in-memory P-256 certificate" do
    {:ok, overrides} =
      ServeTLS.endpoint_overrides(
        env([
          {"SERVE_TLS", "self-signed"},
          {"SERVE_TLS_PORT", "8443"},
          {"SERVE_HTTP_LOOPBACK_ONLY", "true"}
        ])
      )

    assert overrides[:http] == [ip: {127, 0, 0, 1}]
    https = overrides[:https]
    assert https[:port] == 8443
    assert https[:versions] == [:"tlsv1.3", :"tlsv1.2"]
    {type, der} = https[:key]
    key = :public_key.der_decode(type, der)
    assert elem(key, 3) == {:namedCurve, {1, 2, 840, 10045, 3, 1, 7}}
    assert is_binary(https[:cert])
  end

  test "supplied reads files or inline PEM" do
    {:ok, overrides} =
      ServeTLS.endpoint_overrides(
        env([
          {"SERVE_TLS", "supplied"},
          {"SERVE_TLS_PORT", "443"},
          {"SERVE_TLS_CERT_FILE", "/c.pem"},
          {"SERVE_TLS_KEY_FILE", "/k.pem"}
        ])
      )

    assert overrides[:https][:certfile] == "/c.pem"
    assert overrides[:https][:keyfile] == "/k.pem"
    refute Keyword.has_key?(overrides, :http)
  end

  test "loopback HTTP opens with the matching socket family and retains transport settings" do
    values = %{
      "SERVE_TLS" => "self-signed",
      "SERVE_TLS_PORT" => "8443",
      "SERVE_HTTP_LOOPBACK_ONLY" => "true"
    }

    previous = Map.new(values, fn {key, _} -> {key, System.get_env(key)} end)
    System.put_env(values)

    on_exit(fn ->
      Enum.each(previous, fn
        {key, nil} -> System.delete_env(key)
        {key, value} -> System.put_env(key, value)
      end)

      Application.delete_env(:serve_tls_listener_test, __MODULE__)
    end)

    Application.put_env(:serve_tls_listener_test, __MODULE__,
      http: [
        port: 0,
        protocol_options: [invalid_response_headers: :error_terminate],
        transport_options: [num_acceptors: 1, socket_opts: [:inet6, reuseaddr: true]]
      ]
    )

    ServeTLS.configure_endpoint!(:serve_tls_listener_test, __MODULE__)
    http = Application.fetch_env!(:serve_tls_listener_test, __MODULE__)[:http]
    transport = http[:transport_options]
    assert transport[:num_acceptors] == 1
    assert Keyword.get(transport[:socket_opts], :reuseaddr)
    assert http[:protocol_options] == [invalid_response_headers: :error_terminate]

    {:ok, listener} = :gen_tcp.listen(http[:port], [ip: http[:ip]] ++ transport[:socket_opts])
    on_exit(fn -> :gen_tcp.close(listener) end)
    assert {:ok, {{127, 0, 0, 1}, port}} = :inet.sockname(listener)
    assert {:ok, client} = :gen_tcp.connect({127, 0, 0, 1}, port, [], 1_000)
    :gen_tcp.close(client)
  end

  test "a TLS 1.2 client completes a handshake against the listener options" do
    {:ok, overrides} =
      ServeTLS.endpoint_overrides(env([{"SERVE_TLS", "self-signed"}, {"SERVE_TLS_PORT", "1"}]))

    https = overrides[:https]

    {:ok, listen} =
      :ssl.listen(0,
        cert: https[:cert],
        key: https[:key],
        versions: https[:versions],
        reuseaddr: true
      )

    {:ok, {_, port}} = :ssl.sockname(listen)

    test_pid = self()

    # The socket closes with its owner, so the server holds it until the client has looked.
    server =
      Task.async(fn ->
        {:ok, socket} = :ssl.transport_accept(listen)
        result = :ssl.handshake(socket)
        send(test_pid, :handshaken)

        receive do
          :done -> result
        end
      end)

    {:ok, client} =
      :ssl.connect(~c"localhost", port, [versions: [:"tlsv1.2"], verify: :verify_none], 5_000)

    assert_receive :handshaken, 5_000
    assert {:ok, [protocol: :"tlsv1.2"]} = :ssl.connection_information(client, [:protocol])
    send(server.pid, :done)
    assert {:ok, _} = Task.await(server)
    :ssl.close(client)
    :ssl.close(listen)
  end

  test "inline SERVE_TLS_CERT keeps every certificate of the chain" do
    {:ok, generated} =
      ServeTLS.endpoint_overrides(env([{"SERVE_TLS", "self-signed"}, {"SERVE_TLS_PORT", "8443"}]))

    https = generated[:https]
    {key_type, key_der} = https[:key]
    cert = https[:cert]

    chain =
      :public_key.pem_encode([
        {:Certificate, cert, :not_encrypted},
        {:Certificate, cert, :not_encrypted}
      ])

    key = :public_key.pem_encode([{key_type, key_der, :not_encrypted}])

    {:ok, overrides} =
      ServeTLS.endpoint_overrides(
        env([
          {"SERVE_TLS", "supplied"},
          {"SERVE_TLS_PORT", "8443"},
          {"SERVE_TLS_CERT", chain},
          {"SERVE_TLS_KEY", key}
        ])
      )

    assert overrides[:https][:cert] == [https[:cert], https[:cert]]
  end

  test "refuses incomplete settings" do
    for pairs <- [
          [{"SERVE_TLS", "self-signed"}],
          [{"SERVE_TLS", "self-signed"}, {"SERVE_TLS_PORT", "0"}],
          [{"SERVE_TLS", "yes"}, {"SERVE_TLS_PORT", "8443"}],
          [{"SERVE_TLS", "supplied"}, {"SERVE_TLS_PORT", "8443"}, {"SERVE_TLS_CERT", "c"}],
          [{"SERVE_HTTP_LOOPBACK_ONLY", "true"}],
          [{"SERVE_HTTP_LOOPBACK_ONLY", "maybe"}]
        ] do
      assert {:error, _} = ServeTLS.endpoint_overrides(env(pairs)), inspect(pairs)
    end
  end
end
