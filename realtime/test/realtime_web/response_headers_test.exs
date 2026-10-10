defmodule RealtimeWeb.ResponseHeadersTest do
  use ExUnit.Case, async: false

  defmodule HeaderHandler do
    def init(req, state) do
      headers =
        if :cowboy_req.path(req) == "/invalid",
          do: %{"x-test" => "untrusted\r\nx-injected: unsafe"},
          else: %{}

      req = :cowboy_req.reply(200, headers, "ok", req)
      {:ok, req, state}
    end
  end

  test "Cowboy rejects response header injection before writing it to the socket" do
    http = Application.fetch_env!(:realtime, RealtimeWeb.Endpoint)[:http]
    protocol_options = Keyword.fetch!(http, :protocol_options)
    assert protocol_options[:invalid_response_headers] == :error_terminate

    ref = __MODULE__

    dispatch = :cowboy_router.compile([{:_, [{:_, HeaderHandler, []}]}])
    options = protocol_options |> Map.new() |> Map.put(:env, %{dispatch: dispatch})
    {:ok, _} = :cowboy.start_clear(ref, %{socket_opts: [port: 0]}, options)
    on_exit(fn -> :cowboy.stop_listener(ref) end)
    port = :ranch.get_port(ref)

    assert request(port, "/valid") =~ "200 OK"
    response = request(port, "/invalid")
    refute response =~ "x-injected"
    refute response =~ "unsafe"
    refute response =~ "200 OK"
  end

  defp request(port, path) do
    {:ok, socket} = :gen_tcp.connect(~c"localhost", port, [:binary, active: false], 2_000)
    on_exit(fn -> :gen_tcp.close(socket) end)

    :ok =
      :gen_tcp.send(
        socket,
        "GET #{path} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
      )

    receive_response(socket, "")
  end

  defp receive_response(socket, response) do
    case :gen_tcp.recv(socket, 0, 2_000) do
      {:ok, data} -> receive_response(socket, response <> data)
      {:error, :closed} -> response
      {:error, :timeout} -> response
      other -> flunk("unexpected socket result: #{inspect(other)}")
    end
  end
end
