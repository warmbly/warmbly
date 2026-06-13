defmodule Realtime.EventLog do
  @moduledoc """
  Per-organization event log backing resumable delivery on the org channel.

  Every org event is assigned a monotonic per-org sequence number and appended to
  a capped Redis stream BEFORE it is broadcast. A client that reconnects can pass
  the last sequence it saw and have the gap replayed, instead of polling REST to
  resync. If the client was gone long enough that its position fell out of the
  buffer, replay fails and the client must do a full resync.

  Redis-backed and fail-open: if Redis is unavailable, events are still delivered
  live (just without a sequence number, so they are simply not resumable). The
  sequence is GLOBAL per org — every event gets one regardless of which member
  may see it — so replay re-applies the same permission + intent filter as live
  delivery, and a resuming client only ever receives the events it is allowed to.
  """

  alias Realtime.Redis

  require Logger

  # Keep ~this many recent events per org, and let the keys expire if an org goes
  # quiet, so Redis memory stays bounded. A client gone longer than the TTL (or
  # further back than maxlen events) can't resume and does a full resync instead.
  @maxlen 2_000
  @ttl_ms 3_600_000

  # Atomic: bump the per-org sequence, append the event to the capped stream under
  # that sequence as the entry id, refresh both TTLs, return the new sequence.
  @append_script """
  local seq = redis.call('INCR', KEYS[1])
  redis.call('XADD', KEYS[2], 'MAXLEN', '~', tonumber(ARGV[2]), seq .. '-0', 'd', ARGV[1])
  redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[3]))
  redis.call('PEXPIRE', KEYS[2], tonumber(ARGV[3]))
  return seq
  """

  @doc """
  Assign a sequence number to an org event and buffer it for replay, returning the
  event with a `"seq"` field. On Redis failure returns the event unchanged (no
  seq) so live delivery still happens — that one event just isn't resumable.
  """
  def stamp(org_id, event) when is_binary(org_id) and is_map(event) do
    json = Jason.encode!(event)

    case Redis.command([
           "EVAL",
           @append_script,
           2,
           seq_key(org_id),
           buf_key(org_id),
           json,
           @maxlen,
           @ttl_ms
         ]) do
      {:ok, seq} when is_integer(seq) -> Map.put(event, "seq", seq)
      _ -> event
    end
  end

  def stamp(_org_id, event), do: event

  @doc "Current (latest) sequence for an org, or 0 if none / Redis unavailable."
  def current_seq(org_id) do
    case Redis.command(["GET", seq_key(org_id)]) do
      {:ok, v} when is_binary(v) ->
        case Integer.parse(v) do
          {n, _} -> n
          :error -> 0
        end

      _ ->
        0
    end
  end

  @doc """
  Buffered events after `last_seq`, each decoded and re-stamped with its `"seq"`.

    * `{:ok, events}` — the gap is fully covered (possibly empty if caught up)
    * `{:gap, current_seq}` — `last_seq` fell out of the buffer (evicted or the
      counter reset); the caller must do a full resync, not trust a partial replay
  """
  def replay(org_id, last_seq) when is_integer(last_seq) and last_seq >= 0 do
    current = current_seq(org_id)

    cond do
      # Counter went backwards (Redis flushed / key expired then re-created) — we
      # can't prove the client didn't miss anything, so force a full resync.
      current < last_seq ->
        {:gap, current}

      current == last_seq ->
        {:ok, []}

      true ->
        case min_seq(org_id) do
          # Sequence advanced but nothing is buffered (all evicted) -> gap.
          nil -> {:gap, current}
          # The oldest buffered event is newer than last_seq + 1 -> a hole was
          # trimmed away -> gap.
          min when min > last_seq + 1 -> {:gap, current}
          _ -> {:ok, read_after(org_id, last_seq)}
        end
    end
  end

  def replay(org_id, _invalid), do: {:gap, current_seq(org_id)}

  # Private --------------------------------------------------------------------

  defp min_seq(org_id) do
    case Redis.command(["XRANGE", buf_key(org_id), "-", "+", "COUNT", "1"]) do
      {:ok, [[id, _fields] | _]} -> parse_seq(id)
      _ -> nil
    end
  end

  defp read_after(org_id, last_seq) do
    start = "(" <> Integer.to_string(last_seq) <> "-0"

    case Redis.command(["XRANGE", buf_key(org_id), start, "+"]) do
      {:ok, entries} when is_list(entries) ->
        entries
        |> Enum.map(&decode_entry/1)
        |> Enum.reject(&is_nil/1)

      _ ->
        []
    end
  end

  defp decode_entry([id, fields]) do
    with json when is_binary(json) <- field_value(fields, "d"),
         {:ok, event} when is_map(event) <- Jason.decode(json) do
      Map.put(event, "seq", parse_seq(id))
    else
      _ -> nil
    end
  end

  defp decode_entry(_), do: nil

  defp field_value([k, v | _], k), do: v
  defp field_value([_, _ | rest], k), do: field_value(rest, k)
  defp field_value(_, _), do: nil

  defp parse_seq(id) when is_binary(id) do
    id |> String.split("-", parts: 2) |> List.first() |> String.to_integer()
  end

  defp seq_key(org_id), do: "rt:seq:#{org_id}"
  defp buf_key(org_id), do: "rt:buf:#{org_id}"
end
