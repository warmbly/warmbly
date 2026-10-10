defmodule RealtimeWeb.AuthorizationRefreshTest do
  use RealtimeWeb.ChannelCase, async: false

  alias Realtime.Test.AuthRepo
  alias RealtimeWeb.{AccountChannel, CampaignChannel, ChannelGuard, OrgChannel, UserChannel}

  setup do
    start_supervised!(AuthRepo)
    user_id = uuid()
    org_id = uuid()
    campaign_id = uuid()
    account_id = uuid()

    ctx = %{
      user_id: user_id,
      org_id: org_id,
      campaign_id: campaign_id,
      account_id: account_id
    }

    authorize(ctx)
    ctx
  end

  defp authorize(ctx, opts \\ []) do
    role = Keyword.get(opts, :role, "member")
    scope = Keyword.get(opts, :scope, "all")
    campaigns = Keyword.get(opts, :campaigns, [ctx.campaign_id])
    accounts = Keyword.get(opts, :accounts, [ctx.account_id])
    permissions = Keyword.get(opts, :permissions, 65535)
    refusal = Keyword.get(opts, :refusal)

    AuthRepo.respond_with(fn query, params ->
      cond do
        refusal == :error ->
          {:error, :database_unavailable}

        String.contains?(query, "FROM organization_members om") ->
          if refusal == :revoked or params != [bin(ctx.org_id), bin(ctx.user_id)],
            do: rows([]),
            else:
              rows([
                [
                  bin(uuid()),
                  role,
                  permissions,
                  Keyword.get(opts, :show_online, false),
                  true,
                  scope
                ]
              ])

        String.contains?(query, "WITH granted_folders") ->
          if refusal == :scope_error,
            do: {:error, :database_unavailable},
            else: rows([[campaigns, [], accounts]])

        String.contains?(query, "SELECT c.organization_id") or
            String.contains?(query, "SELECT ea.organization_id") ->
          rows([[bin(ctx.org_id)]])

        String.contains?(query, "SELECT first_name, last_name, avatar_url") ->
          rows([["Test", "Member", nil]])

        true ->
          raise "unexpected authorization query: #{query}"
      end
    end)
  end

  defp rows(rows), do: {:ok, %{rows: rows}}
  defp bin(id), do: Ecto.UUID.dump!(id)

  defp join_channel(ctx, :campaign) do
    subscribe_and_join(user_socket(ctx.user_id), CampaignChannel, "campaign:#{ctx.campaign_id}")
  end

  defp join_channel(ctx, :account) do
    subscribe_and_join(user_socket(ctx.user_id), AccountChannel, "account:#{ctx.account_id}")
  end

  defp join_channel(ctx, :org) do
    subscribe_and_join(user_socket(ctx.user_id), OrgChannel, "org:#{ctx.org_id}")
  end

  defp expire(socket) do
    :sys.replace_state(socket.channel_pid, fn state ->
      checked_at = System.monotonic_time(:millisecond) - ChannelGuard.authorization_ttl_ms()
      %{state | assigns: Map.put(state.assigns, :authorization_checked_at, checked_at)}
    end)
  end

  defp broadcast(socket, event) do
    Phoenix.PubSub.broadcast(Realtime.PubSub, socket.topic, {:pubsub_event, event})
  end

  defp event(ctx, :account) do
    %{
      "event_type" => "ACCOUNT_UPDATED",
      "email_account_id" => ctx.account_id,
      "org_id" => ctx.org_id
    }
  end

  defp event(ctx, _) do
    %{
      "event_type" => "CAMPAIGN_UPDATED",
      "campaign_id" => ctx.campaign_id,
      "org_id" => ctx.org_id
    }
  end

  for kind <- [:campaign, :account, :org] do
    @kind kind

    test "#{kind} refreshes stale authorization before delivery", ctx do
      authorize(ctx, scope: "restricted")
      {:ok, _, socket} = join_channel(ctx, @kind)
      expire(socket)
      count = length(AuthRepo.queries())
      ev = event(ctx, @kind)
      type = ev["event_type"]

      broadcast(socket, ev)
      assert_push(^type, ^ev)
      assert length(AuthRepo.queries()) > count
      refreshed = :sys.get_state(socket.channel_pid)

      assert ChannelGuard.fresh?(
               refreshed.assigns.authorization_checked_at,
               System.monotonic_time(:millisecond)
             )
    end

    test "#{kind} uses fresh authorization without another database read", ctx do
      {:ok, _, socket} = join_channel(ctx, @kind)
      count = length(AuthRepo.queries())
      ev = event(ctx, @kind)
      type = ev["event_type"]
      broadcast(socket, ev)
      assert_push(^type, ^ev)
      assert length(AuthRepo.queries()) == count
    end

    for refusal <- [:revoked, :error, :scope_error] do
      @refusal refusal

      test "#{kind} fails closed on stale #{@refusal}", ctx do
        authorize(ctx, scope: "restricted")
        {:ok, _, socket} = join_channel(ctx, @kind)
        expire(socket)
        authorize(ctx, scope: "restricted", refusal: @refusal)
        monitor = Process.monitor(socket.channel_pid)
        broadcast(socket, event(ctx, @kind))

        assert_receive {:DOWN, ^monitor, :process, _pid, :normal}
        refute_push("CAMPAIGN_UPDATED", _)
        refute_push("ACCOUNT_UPDATED", _)
      end
    end

    test "#{kind} periodically reauthorizes even with no event traffic", ctx do
      {:ok, _, socket} = join_channel(ctx, @kind)
      authorize(ctx, refusal: :revoked)
      monitor = Process.monitor(socket.channel_pid)
      send(socket.channel_pid, :refresh_authorization)
      assert_receive {:DOWN, ^monitor, :process, _pid, :normal}
    end

    test "#{kind} updates permissions on a successful stale read", ctx do
      {:ok, _, socket} = join_channel(ctx, @kind)
      expire(socket)
      authorize(ctx, permissions: 0)
      broadcast(socket, event(ctx, @kind))
      refute_push("CAMPAIGN_UPDATED", _)
      refute_push("ACCOUNT_UPDATED", _)
      assert Process.alive?(socket.channel_pid)
      assert :sys.get_state(socket.channel_pid).assigns.permissions == 0
    end

    test "#{kind} applies an unrestricted to restricted transition", ctx do
      {:ok, _, socket} = join_channel(ctx, @kind)
      expire(socket)
      authorize(ctx, scope: "restricted", campaigns: [], accounts: [])
      monitor = Process.monitor(socket.channel_pid)
      broadcast(socket, event(ctx, @kind))

      if @kind == :org do
        assert_push("presence_state", %{})
        assert Process.alive?(socket.channel_pid)
        assert :sys.get_state(socket.channel_pid).assigns.member.scope.campaigns == MapSet.new()
      else
        assert_receive {:DOWN, ^monitor, :process, _pid, :normal}
      end

      refute_push("CAMPAIGN_UPDATED", _)
      refute_push("ACCOUNT_UPDATED", _)
    end
  end

  test "campaign reauthorization rejects a campaign moved out of a granted folder", ctx do
    authorize(ctx, scope: "restricted")
    {:ok, _, socket} = join_channel(ctx, :campaign)
    expire(socket)
    authorize(ctx, scope: "restricted", campaigns: [])
    monitor = Process.monitor(socket.channel_pid)
    broadcast(socket, event(ctx, :campaign))
    assert_receive {:DOWN, ^monitor, :process, _pid, :normal}
    refute_push("CAMPAIGN_UPDATED", _)
  end

  test "account reauthorization rejects a removed mailbox grant", ctx do
    authorize(ctx, scope: "restricted")
    {:ok, _, socket} = join_channel(ctx, :account)
    expire(socket)
    authorize(ctx, scope: "restricted", accounts: [])
    monitor = Process.monitor(socket.channel_pid)
    broadcast(socket, event(ctx, :account))
    assert_receive {:DOWN, ^monitor, :process, _pid, :normal}
    refute_push("ACCOUNT_UPDATED", _)
  end

  test "authorization time is bounded at the exact 30 second boundary" do
    assert ChannelGuard.authorization_ttl_ms() <= 30_000
    assert ChannelGuard.fresh?(10, 30_009)
    refute ChannelGuard.fresh?(10, 30_010)
    refute ChannelGuard.fresh?(11, 10)
    refute ChannelGuard.fresh?(nil, 10)
  end

  test "periodic refresh schedules against the authorization age", ctx do
    checked_at = System.monotonic_time(:millisecond) - 5_000

    socket =
      ChannelGuard.remember_authorization(user_socket(ctx.user_id), %{permissions: 0}, checked_at)

    timer = ChannelGuard.schedule_authorization_refresh(socket)
    assert Process.read_timer(timer) in 24_000..25_000
    Process.cancel_timer(timer)
  end

  test "periodic restriction removes a member's tracked presence", ctx do
    authorize(ctx, show_online: true)
    {:ok, _, socket} = join_channel(ctx, :org)
    assert_push("presence_state", roster)
    assert Map.has_key?(roster, ctx.user_id)
    authorize(ctx, scope: "restricted")
    send(socket.channel_pid, :refresh_authorization)
    assert_push("presence_state", %{})
    assert RealtimeWeb.Presence.list(socket) == %{}
  end

  test "restricted presence uses the actual Phoenix broadcast dispatch", ctx do
    authorize(ctx, scope: "restricted")
    {:ok, _, socket} = join_channel(ctx, :org)

    payload = %{
      joins: %{
        uuid() => %{
          metas: [
            %{
              name: "Private",
              avatar: "private.png",
              page: "campaign",
              resource: uuid(),
              action: "editing"
            }
          ]
        }
      },
      leaves: %{}
    }

    RealtimeWeb.Endpoint.broadcast!(socket.topic, "presence_diff", payload)
    refute_push("presence_diff", _, 100)
    refute_push("presence_state", _, 100)
  end

  test "unrestricted presence still reaches clients via Phoenix broadcast", ctx do
    {:ok, _, socket} = join_channel(ctx, :org)
    assert_push("presence_state", %{})
    payload = %{joins: %{"teammate" => %{metas: [%{name: "Teammate"}]}}, leaves: %{}}
    RealtimeWeb.Endpoint.broadcast!(socket.topic, "presence_diff", payload)
    assert_push("presence_diff", ^payload)
  end

  test "presence broadcasts refresh an unrestricted member before delivery", ctx do
    {:ok, _, socket} = join_channel(ctx, :org)
    assert_push("presence_state", %{})
    expire(socket)
    authorize(ctx, scope: "restricted")

    RealtimeWeb.Endpoint.broadcast!(socket.topic, "presence_diff", %{
      joins: %{"private" => %{}},
      leaves: %{}
    })

    assert_push("presence_state", %{})
    refute_push("presence_diff", _)
  end

  test "presence database errors close the channel without delivering a diff", ctx do
    {:ok, _, socket} = join_channel(ctx, :org)
    assert_push("presence_state", %{})
    expire(socket)
    authorize(ctx, refusal: :error)
    monitor = Process.monitor(socket.channel_pid)

    RealtimeWeb.Endpoint.broadcast!(socket.topic, "presence_diff", %{
      joins: %{"private" => %{}},
      leaves: %{}
    })

    assert_receive {:DOWN, ^monitor, :process, _pid, :normal}
    refute_push("presence_diff", _)
  end

  test "org live frames and policy updates cannot bypass a stale membership refusal", ctx do
    for message <- [
          {:live_event, %{"event_type" => "LIVE_CURSOR", "chat" => "private"}},
          {:pubsub_event,
           %{"event_type" => "PRESENCE_POLICY_UPDATED", "presence_show_online" => true}}
        ] do
      authorize(ctx)
      {:ok, _, socket} = join_channel(ctx, :org)
      assert_push("presence_state", %{})
      expire(socket)
      authorize(ctx, refusal: :revoked)
      monitor = Process.monitor(socket.channel_pid)
      send(socket.channel_pid, message)
      assert_receive {:DOWN, ^monitor, :process, _pid, :normal}
      refute_push("LIVE_CURSOR", _)
    end
  end

  test "a role audit immediately reauthorizes a fresh org channel", ctx do
    {:ok, _, socket} = join_channel(ctx, :org)
    assert_push("presence_state", %{})
    authorize(ctx, refusal: :error)
    monitor = Process.monitor(socket.channel_pid)

    broadcast(socket, %{
      "event_type" => "AUDIT_CREATED",
      "entity_type" => "role",
      "action" => "update"
    })

    assert_receive {:DOWN, ^monitor, :process, _pid, :normal}
    refute_push("AUDIT_CREATED", _)
  end

  test "user channels do not treat a campaign's creator as workspace authority", ctx do
    authorize(ctx, scope: "restricted", campaigns: [])

    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    ev = event(ctx, :campaign) |> Map.put("user_id", ctx.user_id)
    broadcast(socket, ev)
    refute_push("CAMPAIGN_UPDATED", _)
    assert Process.alive?(socket.channel_pid)
  end

  test "user channels deliver granted account-only events without an org field", ctx do
    authorize(ctx, scope: "restricted")

    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    ev = event(ctx, :account) |> Map.delete("org_id")
    broadcast(socket, ev)
    assert_push("ACCOUNT_UPDATED", ^ev)
  end

  test "user channels refuse out-of-scope inbox even when the campaign is granted", ctx do
    authorize(ctx, scope: "restricted", accounts: [])

    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    ev =
      event(ctx, :account)
      |> Map.merge(%{
        "event_type" => "INBOX_EMAIL_RECEIVED",
        "campaign_id" => ctx.campaign_id,
        "subject" => "private"
      })

    broadcast(socket, ev)
    refute_push("INBOX_EMAIL_RECEIVED", _)
  end

  test "user channels require workspace event permissions as well as resource grants", ctx do
    authorize(ctx, scope: "restricted", permissions: 0)

    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    broadcast(socket, event(ctx, :campaign))
    refute_push("CAMPAIGN_UPDATED", _)
  end

  for refusal <- [:revoked, :error] do
    @refusal refusal

    test "user-channel stale #{@refusal} suppresses workspace events but preserves personal control",
         ctx do
      {:ok, _, socket} =
        subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

      ev = event(ctx, :campaign)
      broadcast(socket, ev)
      assert_push("CAMPAIGN_UPDATED", ^ev)

      expire_user_cache(socket)
      authorize(ctx, refusal: @refusal)
      broadcast(socket, ev)
      refute_push("CAMPAIGN_UPDATED", _)
      personal = %{"event_type" => "SESSIONS_REVOKED", "user_id" => ctx.user_id}
      broadcast(socket, personal)
      assert_push("SESSIONS_REVOKED", ^personal)
      assert Process.alive?(socket.channel_pid)
    end
  end

  test "user channels refresh unrestricted to restricted membership on stale delivery", ctx do
    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    ev = event(ctx, :campaign)
    broadcast(socket, ev)
    assert_push("CAMPAIGN_UPDATED", ^ev)
    expire_user_cache(socket)
    authorize(ctx, scope: "restricted", campaigns: [])
    broadcast(socket, ev)
    refute_push("CAMPAIGN_UPDATED", _)
  end

  test "personal notifications remain available without workspace membership", ctx do
    notification_id = uuid()

    AuthRepo.respond_with(fn query, params ->
      assert String.contains?(query, "FROM notifications")
      assert params == [bin(notification_id), bin(ctx.user_id)]
      rows([[nil]])
    end)

    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    ev = %{
      "event_type" => "NOTIFICATION_CREATED",
      "notification_id" => notification_id,
      "category" => "security_new_signin",
      "title" => "New sign-in"
    }

    broadcast(socket, ev)
    assert_push("NOTIFICATION_CREATED", ^ev)
  end

  test "workspace notifications with no resource scope are withheld from restricted owners",
       ctx do
    authorize(ctx, scope: "restricted")
    notification_id = uuid()
    original = Agent.get(AuthRepo, & &1.handler)

    AuthRepo.respond_with(fn query, params ->
      if String.contains?(query, "FROM notifications"),
        do: rows([[bin(ctx.org_id)]]),
        else: original.(query, params)
    end)

    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    ev = %{
      "event_type" => "NOTIFICATION_CREATED",
      "notification_id" => notification_id,
      "category" => "campaign_paused",
      "title" => "Private campaign",
      "link" => "/campaigns/private"
    }

    broadcast(socket, ev)
    refute_push("NOTIFICATION_CREATED", _)
  end

  test "a meeting without org_id is checked through its booking's workspace", ctx do
    authorize(ctx, scope: "restricted", campaigns: [])
    original = Agent.get(AuthRepo, & &1.handler)

    AuthRepo.respond_with(fn query, params ->
      if String.contains?(query, "FROM meeting_bookings"),
        do: rows([[bin(ctx.org_id), bin(ctx.campaign_id)]]),
        else: original.(query, params)
    end)

    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    ev = %{
      "event_type" => "MEETING_BOOKED",
      "booking_id" => uuid(),
      "contact_id" => uuid(),
      "invitee_name" => "Private contact"
    }

    broadcast(socket, ev)
    refute_push("MEETING_BOOKED", _)
    authorize(ctx, scope: "restricted")
    original = Agent.get(AuthRepo, & &1.handler)

    AuthRepo.respond_with(fn query, params ->
      if String.contains?(query, "FROM meeting_bookings"),
        do: rows([[bin(ctx.org_id), bin(ctx.campaign_id)]]),
        else: original.(query, params)
    end)

    expire_user_cache(socket)
    broadcast(socket, ev)
    assert_push("MEETING_BOOKED", ^ev)
  end

  test "events without workspace authority fail closed", ctx do
    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    broadcast(socket, %{
      "event_type" => "INBOX_EMAIL_RECEIVED",
      "user_id" => ctx.user_id,
      "subject" => "private"
    })

    refute_push("INBOX_EMAIL_RECEIVED", _)
  end

  test "user-channel fresh cache delivers once and stale success rereads authorization", ctx do
    authorize(ctx, scope: "restricted")

    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    ev = event(ctx, :account)
    broadcast(socket, ev)
    assert_push("ACCOUNT_UPDATED", ^ev)
    refute_push("ACCOUNT_UPDATED", _)
    count = length(AuthRepo.queries())
    broadcast(socket, ev)
    assert_push("ACCOUNT_UPDATED", ^ev)
    assert length(AuthRepo.queries()) == count
    expire_user_cache(socket)
    broadcast(socket, ev)
    assert_push("ACCOUNT_UPDATED", ^ev)
    assert length(AuthRepo.queries()) > count
  end

  test "user channels refuse events whose resource moved to another workspace", ctx do
    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    ev = event(ctx, :campaign) |> Map.put("org_id", uuid())
    broadcast(socket, ev)
    refute_push("CAMPAIGN_UPDATED", _)
  end

  test "user-channel resource caches stay bounded", ctx do
    {:ok, _, socket} =
      subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

    for _ <- 1..129 do
      ev = event(ctx, :campaign) |> Map.put("campaign_id", uuid())
      broadcast(socket, ev)
      assert_push("CAMPAIGN_UPDATED", ^ev)
    end

    assert map_size(:sys.get_state(socket.channel_pid).assigns.event_authorizations) <= 128
  end

  for kind <- [:campaign, :account] do
    @kind kind

    test "legacy #{@kind} direct ownership works on resource and user channels", ctx do
      AuthRepo.respond_with(fn query, params ->
        cond do
          String.contains?(query, "SELECT c.organization_id") or
              String.contains?(query, "SELECT ea.organization_id") ->
            rows([[nil]])

          String.contains?(query, "AND c.user_id = $2") or
              String.contains?(query, "AND ea.user_id = $2") ->
            if List.last(params) == bin(ctx.user_id),
              do: rows([[List.first(params)]]),
              else: rows([])

          true ->
            raise "unexpected legacy query: #{query}"
        end
      end)

      {:ok, _, resource_socket} = join_channel(ctx, @kind)
      ev = event(ctx, @kind) |> Map.delete("org_id")
      type = ev["event_type"]
      broadcast(resource_socket, ev)
      assert_push(^type, ^ev)
      expire(resource_socket)
      broadcast(resource_socket, ev)
      assert_push(^type, ^ev)

      {:ok, _, socket} =
        subscribe_and_join(user_socket(ctx.user_id), UserChannel, "user:#{ctx.user_id}")

      broadcast(socket, ev)
      assert_push(^type, ^ev)
    end
  end

  defp expire_user_cache(socket) do
    :sys.replace_state(socket.channel_pid, fn state ->
      checked_at = System.monotonic_time(:millisecond) - ChannelGuard.authorization_ttl_ms()

      cache =
        Map.new(state.assigns.event_authorizations, fn {key, {_time, member}} ->
          {key, {checked_at, member}}
        end)

      %{state | assigns: Map.put(state.assigns, :event_authorizations, cache)}
    end)
  end
end
