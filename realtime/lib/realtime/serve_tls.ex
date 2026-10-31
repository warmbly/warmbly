defmodule Realtime.ServeTLS do
  @moduledoc "Opt-in TLS listener beside the plain one, read from SERVE_TLS; unset, the endpoint is untouched."

  @versions [:"tlsv1.3", :"tlsv1.2"]

  @doc "The endpoint keys to override for this environment, or an error naming the bad setting."
  def endpoint_overrides(getenv) do
    get = fn name ->
      case getenv.(name) do
        value when is_binary(value) -> if String.trim(value) == "", do: nil, else: value
        _ -> nil
      end
    end

    with {:ok, loopback_only} <- loopback_only(get),
         {:ok, mode} <- mode(get, loopback_only) do
      case mode do
        nil -> {:ok, []}
        mode -> https(get, mode, loopback_only)
      end
    end
  end

  @doc "Applies endpoint_overrides/1 to the endpoint's application env; raises on a bad setting."
  def configure_endpoint!(app \\ :realtime, endpoint \\ RealtimeWeb.Endpoint) do
    case endpoint_overrides(&System.get_env/1) do
      {:ok, []} ->
        :ok

      {:ok, overrides} ->
        config = Application.get_env(app, endpoint, [])
        Application.put_env(app, endpoint, Keyword.merge(config, overrides, &merge_listener/3))

      {:error, message} ->
        raise message
    end
  end

  defp merge_listener(_key, old, new) when is_list(old) and is_list(new),
    do: Keyword.merge(old, new)

  defp merge_listener(_key, _old, new), do: new

  defp loopback_only(get) do
    case get.("SERVE_HTTP_LOOPBACK_ONLY") do
      nil -> {:ok, false}
      v when v in ["true", "1"] -> {:ok, true}
      v when v in ["false", "0"] -> {:ok, false}
      v -> {:error, "SERVE_HTTP_LOOPBACK_ONLY must be true or false, got #{inspect(v)}"}
    end
  end

  defp mode(get, loopback_only) do
    case get.("SERVE_TLS") && String.downcase(String.trim(get.("SERVE_TLS"))) do
      off when off in [nil, "off", "false"] ->
        if loopback_only,
          do:
            {:error,
             "SERVE_HTTP_LOOPBACK_ONLY=true needs SERVE_TLS, or nothing outside the container can reach the service"},
          else: {:ok, nil}

      "self-signed" ->
        {:ok, :self_signed}

      "supplied" ->
        {:ok, :supplied}

      other ->
        {:error, "SERVE_TLS must be self-signed or supplied, got #{inspect(other)}"}
    end
  end

  defp https(get, mode, loopback_only) do
    with {:ok, port} <- port(get),
         {:ok, material} <- material(get, mode) do
      plain = if loopback_only, do: [http: [ip: {127, 0, 0, 1}]], else: []

      tls =
        [ip: {0, 0, 0, 0, 0, 0, 0, 0}, port: port, versions: @versions] ++
          material ++
          [protocol_options: [invalid_response_headers: :error_terminate]]

      {:ok, plain ++ [https: tls]}
    end
  end

  defp port(get) do
    case Integer.parse(String.trim(get.("SERVE_TLS_PORT") || "")) do
      {port, ""} when port in 1..65_535 -> {:ok, port}
      _ -> {:error, "SERVE_TLS needs SERVE_TLS_PORT set to a port number"}
    end
  end

  defp material(_get, :self_signed) do
    today = Date.utc_today()
    from = Date.add(today, -1) |> Date.to_erl()
    to = Date.add(today, 365) |> Date.to_erl()

    %{cert: cert, key: key} =
      :public_key.pkix_test_root_cert(~c"localhost", [
        {:key, {:namedCurve, :secp256r1}},
        {:digest, :sha256},
        {:validity, {from, to}}
      ])

    {:ok, [cert: cert, key: {:ECPrivateKey, :public_key.der_encode(:ECPrivateKey, key)}]}
  end

  defp material(get, :supplied) do
    with {:ok, cert} <- source(get, "SERVE_TLS_CERT"),
         {:ok, key} <- source(get, "SERVE_TLS_KEY") do
      {:ok, cert_option(cert) ++ key_option(key)}
    end
  end

  defp source(get, name) do
    case {get.(name), get.(name <> "_FILE")} do
      {pem, nil} when is_binary(pem) -> {:ok, {:pem, pem}}
      {nil, file} when is_binary(file) -> {:ok, {:file, String.trim(file)}}
      _ -> {:error, "SERVE_TLS=supplied needs exactly one of #{name} and #{name}_FILE"}
    end
  end

  defp cert_option({:file, path}), do: [certfile: path]

  defp cert_option({:pem, pem}) do
    case for({:Certificate, der, _} <- :public_key.pem_decode(pem), do: der) do
      [leaf | _] -> [cert: leaf]
      [] -> raise "SERVE_TLS_CERT holds no PEM certificate"
    end
  end

  defp key_option({:file, path}), do: [keyfile: path]

  defp key_option({:pem, pem}) do
    case :public_key.pem_decode(pem) do
      [{type, der, :not_encrypted} | _] -> [key: {type, der}]
      _ -> raise "SERVE_TLS_KEY holds no unencrypted PEM key"
    end
  end
end
