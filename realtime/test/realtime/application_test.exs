defmodule Realtime.ApplicationTest do
  use ExUnit.Case, async: true

  describe "redact_userinfo/1" do
    # This ran on every boot with the real password in it. The address has to
    # survive, because naming which Redis the node attached to is the whole
    # point of the line.
    test "strips the credential but keeps the address" do
      redacted =
        Realtime.Application.redact_userinfo(
          "rediss://default:supersecrettoken@optimal-poodle-166045.upstash.io:6379"
        )

      refute redacted =~ "supersecrettoken"
      assert redacted == "rediss://***@optimal-poodle-166045.upstash.io:6379"
    end

    test "a url with no credential is untouched" do
      assert Realtime.Application.redact_userinfo("redis://localhost:6379/0") ==
               "redis://localhost:6379/0"
    end

    test "the not-configured fallback survives" do
      assert Realtime.Application.redact_userinfo("not configured") == "not configured"
    end

    test "a username-only credential still goes" do
      redacted = Realtime.Application.redact_userinfo("redis://someuser@redis.internal:6379")
      refute redacted =~ "someuser"
    end
  end
end
