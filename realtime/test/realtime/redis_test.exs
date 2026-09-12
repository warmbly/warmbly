defmodule Realtime.RedisTest do
  use ExUnit.Case, async: true

  alias Realtime.Redis

  describe "tls_opts/1" do
    test "plain redis:// gets no socket options" do
      assert Redis.tls_opts("redis://localhost:6379/0") == []
    end

    # Every managed Redis presents a wildcard certificate (*.upstash.io and
    # friends), and Erlang's default hostname check rejects one. Without this
    # the whole pool fails to connect with {:bad_cert, :hostname_check_failed}.
    test "rediss:// asks for the https wildcard match fun" do
      opts = Redis.tls_opts("rediss://user:pass@optimal-poodle-166045.upstash.io:6379")

      assert [socket_opts: socket_opts] = opts
      assert [customize_hostname_check: [match_fun: match_fun]] = socket_opts
      assert is_function(match_fun, 2)
    end

    # The returned match fun has to actually accept a wildcard, which is the
    # behaviour this exists for; asserting only on shape would pass with the
    # wrong :public_key helper.
    test "the match fun accepts a wildcard certificate for the host" do
      [socket_opts: [customize_hostname_check: [match_fun: match_fun]]] =
        Redis.tls_opts("rediss://example.upstash.io:6379")

      assert match_fun.({:dns_id, ~c"example.upstash.io"}, {:dNSName, ~c"*.upstash.io"})
    end
  end
end
