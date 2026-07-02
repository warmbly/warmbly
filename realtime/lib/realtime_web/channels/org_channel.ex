defmodule RealtimeWeb.OrgChannel do
  @moduledoc """
  Channel for organization-specific events and team presence.

  Users join their organization's channel to receive org-scoped dashboard
  events (campaign sends, inbox arrivals, audit entries, member changes, ...)
  filtered by their member permissions.

  Presence: every JWT member is tracked in `RealtimeWeb.Presence` with
  display metadata and a live activity descriptor. Clients push
  `presence:update` (rate-limited like any client event) with:

      %{"page" => "/app/unibox", "resource" => "thread:<id>", "action" => "replying"}

  so teammates see who is online, who is viewing the same record, and who is
  already replying to an email. API-key (developer) sockets receive events but
  are never tracked as presences.
  """

  use Phoenix.Channel

  require Logger

  alias Realtime.Auth
  alias Realtime.Connections
  alias Realtime.RateLimiter
  alias RealtimeWeb.Presence

  @presence_actions ~w(viewing editing replying idle)

  # Ephemeral live-collaboration frames (cursor moves, card drags, selections)
  # on a shared canvas like the automation builder. They are far too frequent
  # for the per-minute ws_event / ws_message Redis limiters and carry no durable
  # value, so they ride a separate fast path: a cheap in-process token bucket on
  # the sending socket, a direct PubSub fan-out that skips the
  # sequenced/resumable event log, and best-effort delivery (a dropped frame is
  # fine — the next one is milliseconds away).
  @live_kinds ~w(cursor node patch select)
  @live_rate 50.0
  @live_burst 60.0

  @impl true
  def join("org:" <> org_id, params, socket) do
    user_id = socket.assigns.user_id

    case Auth.check_org_membership(user_id, org_id) do
      {:ok, member} ->
        Logger.debug("User #{user_id} joined org channel #{org_id}")

        socket =
          socket
          |> assign(:org_id, org_id)
          |> assign(:member, member)
          |> assign(:permissions, Map.get(member, :permissions, 0))
          # Org-wide presence privacy, read once at join. Re-read on rejoin, so a
          # settings change applies live once clients reconnect to the channel.
          |> assign(:presence_show_online, Map.get(member, :presence_show_online, true))
          |> assign(:presence_show_activity, Map.get(member, :presence_show_activity, true))
          # Optional event-family intents: a client may join with
          # {"intents": ["AUDIT", "CAMPAIGN"]} to receive only matching event
          # types. Absent or empty means the full org stream (back-compatible).
          |> assign(:intents, parse_intents(params))
          # Resume token from a reconnecting client: the last sequence it saw.
          |> assign(:resume_from, parse_resume(params))

        send(self(), :after_join)

        # The join reply doubles as a HELLO: advertise the heartbeat cadence the
        # server expects (so library authors do not hardcode it) and the current
        # stream sequence. Every event carries a monotonic `seq`; track the
        # highest you have seen and rejoin with {"resume": {"last_seq": seq}} to
        # replay what you missed across a disconnect. Heartbeats are
        # client-initiated on the "phoenix" topic; send one within
        # server_timeout_ms or the socket is closed.
        {:ok,
         %{
           org_id: org_id,
           role: member.role,
           heartbeat_interval_ms: 25_000,
           server_timeout_ms: 60_000,
           seq: Realtime.EventLog.current_seq(org_id),
           resume_supported: true
         }, socket}

      {:error, :not_a_member} ->
        join_error(:not_a_member)

      {:error, reason} ->
        Logger.warning("Failed to join org channel: #{inspect(reason)}")
        join_error(reason)
    end
  end

  # Structured join rejection the client can branch on: a numeric `code`
  # (Auth.error_code/1) plus a human-readable `reason` (Auth.error_message/1),
  # mirroring the socket-level auth error shape.
  defp join_error(reason) do
    {:error, %{code: Auth.error_code(reason), reason: Auth.error_message(reason)}}
  end

  @impl true
  def handle_info(:after_join, socket) do
    # Subscribe to the organization's Pub/Sub topic
    org_id = socket.assigns.org_id
    Phoenix.PubSub.subscribe(Realtime.PubSub, "org:#{org_id}")

    # Track presence for human members only; developer API-key sockets are
    # event consumers, not teammates. When the org has turned off "show who's
    # online", we track no one — so nobody appears online for anybody — but
    # still push the (empty) roster so the client's join-complete logic runs.
    if Map.get(socket.assigns, :auth_type) == :jwt do
      if socket.assigns[:presence_show_online] do
        profile = Auth.get_user_profile(socket.assigns.user_id)

        {:ok, _} =
          Presence.track(socket, socket.assigns.user_id, %{
            online_at: System.system_time(:second),
            name: profile.name,
            avatar: profile.avatar,
            page: nil,
            resource: nil,
            action: nil
          })
      end

      push(socket, "presence_state", Presence.list(socket))
    end

    # If the client reconnected with a resume token, replay the events it missed
    # (re-applying the same permission + intent filter as live delivery), or tell
    # it the buffer no longer covers its position so it should do a full resync.
    maybe_replay(socket)

    {:noreply, socket}
  end

  # Org-wide presence privacy changed. Re-gate THIS socket live instead of
  # waiting for a reconnect: drop the current presence, then re-add it only if
  # "show online" is now on (re-added with nil activity, which also enforces
  # "activity off" until the next — stripped — client push). Phoenix broadcasts
  # the resulting presence diff, so every teammate's UI updates immediately.
  # Not forwarded to web clients (the audit event refreshes the settings UI).
  @impl true
  def handle_info({:pubsub_event, %{"event_type" => "PRESENCE_POLICY_UPDATED"} = event}, socket) do
    show_online = event["presence_show_online"] != false
    show_activity = event["presence_show_activity"] != false

    socket =
      socket
      |> assign(:presence_show_online, show_online)
      |> assign(:presence_show_activity, show_activity)

    if Map.get(socket.assigns, :auth_type) == :jwt do
      Presence.untrack(socket, socket.assigns.user_id)

      if show_online do
        profile = Auth.get_user_profile(socket.assigns.user_id)

        Presence.track(socket, socket.assigns.user_id, %{
          online_at: System.system_time(:second),
          name: profile.name,
          avatar: profile.avatar,
          page: nil,
          resource: nil,
          action: nil
        })
      end
    end

    {:noreply, socket}
  end

  # Ephemeral live-collaboration fan-out (cursor / card drag). Pushed straight to
  # the client with no ws_message rate limit (the sender's token bucket already
  # bounds volume) and no sequence number (these are never resumed). Only human
  # (JWT) members are collaborators; developer API-key sockets ignore them.
  @impl true
  def handle_info({:live_event, event}, socket) do
    if Map.get(socket.assigns, :auth_type) == :jwt do
      push(socket, event["event_type"], event)
    end

    {:noreply, socket}
  end

  @impl true
  def handle_info({:pubsub_event, event}, socket) do
    # Rate limit outbound messages
    user_id = socket.assigns.user_id
    limits = Map.get(socket.assigns, :rate_limits, %{})
    ws_message_limit = Map.get(limits, :limit_ws_message_pm)

    case RateLimiter.check(user_id, :ws_message, ws_message_limit) do
      {:ok, _remaining} ->
        # Forward only if the member may see it AND it matches the client's
        # declared intents (if any).
        if can_see_event?(socket, event) and intents_allow?(socket, event) do
          push(socket, event["event_type"], event)
        end

      {:error, :rate_limited, retry_after_ms} ->
        push(socket, "rate_limited", %{
          category: "ws_message",
          retry_after_ms: retry_after_ms
        })

        Logger.debug("User #{user_id} rate limited on ws_message")
    end

    {:noreply, socket}
  end

  # Swallow the duplicate %Broadcast{} our manual PubSub subscription delivers
  # to the channel process (the fastlane copy is what reaches the client).
  @impl true
  def handle_info(%Phoenix.Socket.Broadcast{}, socket), do: {:noreply, socket}

  # Presence diffs arrive as channel out-events. Phoenix routes them to
  # handle_out/3, so we must define it (its absence crashed the channel and
  # dropped the socket). Push presence_state/presence_diff straight to the
  # client — they are low-volume and not permission-sensitive.
  @impl true
  def handle_out(event, payload, socket) do
    push(socket, event, payload)
    {:noreply, socket}
  end

  @impl true
  def handle_in("ping", _payload, socket) do
    {:reply, {:ok, %{pong: System.system_time(:millisecond)}}, socket}
  end

  # Ephemeral live-collaboration frame (cursor move / card drag / selection).
  # Bypasses the Redis ws_event limiter for an in-process token bucket, then fans out to the
  # org's other sockets without a sequence number (so it is never replayed on
  # resume). Best-effort: over-budget frames are dropped silently.
  @impl true
  def handle_in("live:" <> kind, payload, socket) when kind in @live_kinds do
    cond do
      Map.get(socket.assigns, :auth_type) != :jwt ->
        {:noreply, socket}

      not socket.assigns[:presence_show_online] or not socket.assigns[:presence_show_activity] ->
        # The org has hidden this member's live activity; suppress their cursor
        # and drag frames too, mirroring presence:update's privacy gate.
        {:noreply, socket}

      true ->
        case take_live_token(socket) do
          {:ok, socket} ->
            case build_live_event(kind, payload, socket) do
              nil ->
                :ok

              event ->
                Phoenix.PubSub.broadcast_from(
                  Realtime.PubSub,
                  self(),
                  "org:#{socket.assigns.org_id}",
                  {:live_event, event}
                )
            end

            {:noreply, socket}

          {:drop, socket} ->
            {:noreply, socket}
        end
    end
  end

  @impl true
  def handle_in(event, payload, socket) do
    # Rate limit client-sent events
    user_id = socket.assigns.user_id
    limits = Map.get(socket.assigns, :rate_limits, %{})
    ws_event_limit = Map.get(limits, :limit_ws_event_pm)

    case RateLimiter.check(user_id, :ws_event, ws_event_limit) do
      {:ok, _remaining} ->
        handle_client_event(event, payload, socket)

      {:error, :rate_limited, retry_after_ms} ->
        {:reply,
         {:error,
          %{
            reason: "rate_limited",
            category: "ws_event",
            retry_after_ms: retry_after_ms
          }}, socket}
    end
  end

  @impl true
  def terminate(_reason, socket) do
    ip = Map.get(socket.assigns, :ip_address)
    Connections.untrack(socket.assigns.user_id, ip: ip)
    :ok
  end

  # Private functions

  # Normalize the optional intents list into upcased family tokens, or nil for
  # "everything". Each token is substring-matched against the event type, so
  # "CAMPAIGN" matches CAMPAIGN_* and "AUDIT" matches AUDIT_CREATED.
  defp parse_intents(params) when is_map(params) do
    case params["intents"] do
      list when is_list(list) ->
        tokens =
          list
          |> Enum.filter(&is_binary/1)
          |> Enum.map(fn s -> s |> String.upcase() |> String.replace(~r/[.:\s-]+/, "_") end)
          |> Enum.reject(&(&1 == ""))

        if tokens == [], do: nil, else: tokens

      _ ->
        nil
    end
  end

  defp parse_intents(_), do: nil

  # Resume support -------------------------------------------------------------

  # A reconnecting client may join with {"resume": {"last_seq": <n>}} to replay
  # the events it missed. Returns the sequence (>= 0), :invalid for a malformed
  # token, or nil for a fresh (non-resuming) join.
  defp parse_resume(params) when is_map(params) do
    case params["resume"] do
      %{"last_seq" => v} -> normalize_seq(v)
      _ -> nil
    end
  end

  defp parse_resume(_), do: nil

  defp normalize_seq(v) when is_integer(v) and v >= 0, do: v

  defp normalize_seq(v) when is_binary(v) do
    case Integer.parse(v) do
      {n, ""} when n >= 0 -> n
      _ -> :invalid
    end
  end

  defp normalize_seq(_), do: :invalid

  # Replay the gap for a resuming client, or signal that a full resync is needed.
  # Runs after the PubSub subscribe, so a live event landing during replay may be
  # delivered twice (once here, once live) — resume is at-least-once, so clients
  # dedupe by `seq`. The replay is bounded by the buffer size.
  defp maybe_replay(socket) do
    org_id = socket.assigns.org_id

    case socket.assigns[:resume_from] do
      nil ->
        :ok

      :invalid ->
        push(socket, "resume_failed", %{
          reason: "invalid_resume",
          current_seq: Realtime.EventLog.current_seq(org_id)
        })

      last_seq ->
        case Realtime.EventLog.replay(org_id, last_seq) do
          {:ok, events} ->
            replayed =
              Enum.reduce(events, 0, fn ev, n ->
                if can_see_event?(socket, ev) and intents_allow?(socket, ev) do
                  push(socket, ev["event_type"], ev)
                  n + 1
                else
                  n
                end
              end)

            push(socket, "resumed", %{
              from: last_seq,
              current_seq: Realtime.EventLog.current_seq(org_id),
              replayed: replayed
            })

          {:gap, current} ->
            push(socket, "resume_failed", %{reason: "buffer_evicted", current_seq: current})
        end
    end

    :ok
  end

  # When a client declared intents, forward only events whose normalized type
  # contains one of the requested tokens. No intents = forward everything.
  defp intents_allow?(socket, event) do
    case socket.assigns[:intents] do
      nil ->
        true

      tokens ->
        type =
          event
          |> Map.get("event_type", "")
          |> to_string()
          |> String.upcase()
          |> String.replace(~r/[.:\s-]+/, "_")

        Enum.any?(tokens, fn t -> String.contains?(type, t) end)
    end
  end

  # Gate org-broadcast events on member permissions. Event types are
  # normalized (upcased, separators collapsed to "_") so both the legacy
  # lowercase names and the Go publisher's UPPER_SNAKE names match.
  defp can_see_event?(socket, event) do
    event_type =
      event
      |> Map.get("event_type", "")
      |> to_string()
      |> String.upcase()
      |> String.replace(~r/[.:\s-]+/, "_")

    permissions = socket.assigns.permissions
    has = fn perm -> Auth.has_permission?(%{permissions: permissions}, Auth.permission(perm)) end

    cond do
      # Billing
      String.contains?(event_type, "SUBSCRIPTION") or String.contains?(event_type, "BILLING") ->
        has.(:manage_billing)

      # Team / member events
      String.contains?(event_type, "MEMBER") or String.contains?(event_type, "INVITATION") ->
        has.(:manage_team)

      # Org settings changes
      String.contains?(event_type, "SETTINGS") ->
        has.(:manage_settings)

      # Unibox rows carry subject + preview snippets
      String.contains?(event_type, "INBOX") or
          event_type in ["EMAIL_RECEIVED", "EMAIL_UPDATED", "EMAIL_DELETED"] ->
        has.(:access_unibox)

      # Campaign activity: lifecycle, task progress, send/open/click/reply pulses
      String.contains?(event_type, "CAMPAIGN") or String.contains?(event_type, "TASK_PROGRESS") or
          event_type in [
            "EMAIL_SENT",
            "EMAIL_OPENED",
            "EMAIL_CLICKED",
            "EMAIL_REPLIED",
            "EMAIL_BOUNCED"
          ] ->
        has.(:view_campaigns)

      # Contact changes
      String.contains?(event_type, "CONTACT") ->
        has.(:view_contacts)

      # Mailbox account + warmup health transitions
      String.contains?(event_type, "ACCOUNT") or String.contains?(event_type, "WARMUP") ->
        has.(:manage_emails)

      # Developer "fire event" custom events: the org's own automation/campaign
      # signals, with no per-app context on this socket. Allow to any subscriber
      # on the org channel (the same join already verified org membership). An
      # "intents" of "CUSTOM" filters these in: the substring match covers
      # "CUSTOM_EVENT" since "CUSTOM" is a prefix of the normalized type.
      event_type == "CUSTOM_EVENT" ->
        true

      # Default: allow (audit refresh signals, meetings, automations, ...).
      # The corresponding list endpoints enforce their own permissions; these
      # events only tell the dashboard to refetch.
      true ->
        true
    end
  end

  # presence:update — merge the client's sanitized activity descriptor into its
  # presence meta. Only tracked (JWT) members can update presence. Gated by the
  # org privacy policy: if "show who's online" is off the member isn't tracked
  # (nothing to update); if "show activity" is off we keep them online but strip
  # the viewing/editing/page detail so teammates never see what they're doing.
  defp handle_client_event("presence:update", payload, socket) do
    if Map.get(socket.assigns, :auth_type) == :jwt and socket.assigns[:presence_show_online] do
      patch =
        if socket.assigns[:presence_show_activity] do
          sanitize_presence(payload)
        else
          %{page: nil, resource: nil, action: nil, updated_at: System.system_time(:second)}
        end

      Presence.update(socket, socket.assigns.user_id, fn meta -> Map.merge(meta, patch) end)
    end

    {:noreply, socket}
  end

  defp handle_client_event(_event, _payload, socket) do
    # Default handler for unknown events
    {:noreply, socket}
  end

  defp sanitize_presence(payload) when is_map(payload) do
    action =
      case payload["action"] do
        a when a in @presence_actions -> a
        _ -> nil
      end

    %{
      page: presence_string(payload["page"]),
      resource: presence_string(payload["resource"]),
      action: action,
      updated_at: System.system_time(:second)
    }
  end

  defp sanitize_presence(_), do: %{page: nil, resource: nil, action: nil}

  # In-process token bucket on the sending socket. Refills at @live_rate tokens
  # per second up to @live_burst; each accepted frame spends one. Cheap (no
  # Redis) and per-socket, so one hot dragger throttles only itself.
  defp take_live_token(socket) do
    now = System.monotonic_time(:millisecond)

    %{tokens: tokens, last: last} =
      Map.get(socket.assigns, :live_bucket, %{tokens: @live_burst, last: now})

    tokens = min(@live_burst, tokens + (now - last) / 1000 * @live_rate)

    if tokens >= 1.0 do
      {:ok, assign(socket, :live_bucket, %{tokens: tokens - 1.0, last: now})}
    else
      {:drop, assign(socket, :live_bucket, %{tokens: tokens, last: now})}
    end
  end

  # Build the broadcast body for a live frame, or nil when it lacks the resource
  # (and, for a node move, the node id) the receiver needs to scope it.
  defp build_live_event("cursor", payload, socket) do
    case presence_string(payload["resource"]) do
      nil ->
        nil

      resource ->
        %{
          "event_type" => "LIVE_CURSOR",
          "user_id" => socket.assigns.user_id,
          "org_id" => socket.assigns.org_id,
          "resource" => resource,
          "x" => num(payload["x"]),
          "y" => num(payload["y"]),
          # Optional cursor-chat text riding the frame; nil when not chatting.
          "chat" => chat_string(payload["chat"]),
          "gone" => payload["gone"] == true,
          "ts" => System.system_time(:millisecond)
        }
    end
  end

  defp build_live_event("node", payload, socket) do
    resource = presence_string(payload["resource"])
    id = presence_string(payload["id"])

    if resource && id do
      %{
        "event_type" => "LIVE_NODE",
        "user_id" => socket.assigns.user_id,
        "org_id" => socket.assigns.org_id,
        "resource" => resource,
        "id" => id,
        "x" => num(payload["x"]),
        "y" => num(payload["y"]),
        "dragging" => payload["dragging"] == true,
        "ts" => System.system_time(:millisecond)
      }
    end
  end

  # Generic collaborative-state hint (e.g. a deal moved to a stage). Carries a
  # small sanitized data map so teammates on the same resource can update
  # optimistically before the durable audit refetch lands. Coordinate-free.
  defp build_live_event("patch", payload, socket) do
    resource = presence_string(payload["resource"])
    data = sanitize_data(payload["data"])

    if resource && map_size(data) > 0 do
      %{
        "event_type" => "LIVE_PATCH",
        "user_id" => socket.assigns.user_id,
        "org_id" => socket.assigns.org_id,
        "resource" => resource,
        "data" => data,
        "ts" => System.system_time(:millisecond)
      }
    end
  end

  # Canvas selection: which node ids this member currently has selected on the
  # shared surface. An empty list is meaningful (deselected), so it broadcasts.
  defp build_live_event("select", payload, socket) do
    case presence_string(payload["resource"]) do
      nil ->
        nil

      resource ->
        %{
          "event_type" => "LIVE_SELECT",
          "user_id" => socket.assigns.user_id,
          "org_id" => socket.assigns.org_id,
          "resource" => resource,
          "ids" => sanitize_ids(payload["ids"]),
          "ts" => System.system_time(:millisecond)
        }
    end
  end

  defp build_live_event(_kind, _payload, _socket), do: nil

  # Keep a selection list small and string-only: ids capped in length and count.
  # The hard cap comes first so an oversized payload is never fully traversed.
  defp sanitize_ids(list) when is_list(list) do
    list
    |> Enum.take(150)
    |> Enum.filter(&is_binary/1)
    |> Enum.map(&String.slice(&1, 0, 64))
    |> Enum.reject(&(&1 == ""))
    |> Enum.take(100)
  end

  defp sanitize_ids(_), do: []

  # Cursor-chat text: trimmed-empty and non-strings become nil (no chat).
  defp chat_string(v) when is_binary(v) do
    case String.trim(v) do
      "" -> nil
      _ -> String.slice(v, 0, 120)
    end
  end

  defp chat_string(_), do: nil

  # Keep a live:patch payload small and scalar-only: at most 20 keys, string
  # values capped, numbers/booleans passed through, everything else dropped.
  defp sanitize_data(m) when is_map(m) do
    m
    |> Enum.take(20)
    |> Enum.reduce(%{}, fn {k, v}, acc ->
      key = presence_string(k)

      case {key, scalar(v)} do
        {nil, _} -> acc
        {_, :drop} -> acc
        {key, val} -> Map.put(acc, key, val)
      end
    end)
  end

  defp sanitize_data(_), do: %{}

  defp scalar(v) when is_binary(v), do: String.slice(v, 0, 200)
  defp scalar(v) when is_number(v), do: v
  defp scalar(v) when is_boolean(v), do: v
  defp scalar(_), do: :drop

  defp num(v) when is_number(v), do: v
  defp num(_), do: 0

  defp presence_string(value) when is_binary(value) do
    case String.trim(value) do
      "" -> nil
      trimmed -> String.slice(trimmed, 0, 160)
    end
  end

  defp presence_string(_), do: nil
end
