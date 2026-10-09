defmodule RealtimeWeb.OrgChannelScopeTest do
  @moduledoc """
  Which org events reach a member restricted to some campaigns and mailboxes.
  """

  use ExUnit.Case, async: true

  alias Realtime.Auth
  alias RealtimeWeb.OrgChannel

  @me "0b7d5c1e-6a1f-4c1e-9d5a-2f0c8e4b7a10"
  @campaign "1c8e6d2f-7b2a-4d2f-8e6b-3a1d9f5c8b21"
  @other_campaign "2d9f7e3a-8c3b-4e3a-9f7c-4b2eaa6d9c32"
  @mailbox "3eaa8f4b-9d4c-4f4b-8a8d-5c3fbb7eac43"
  @other_mailbox "4fbb9a5c-ae5d-4a5c-9b9e-6d4acc8fbd54"
  @folder "5acc0b6d-bf6e-4b6d-8caf-7e5bdd9acf65"

  @member %{
    scope: %{
      campaigns: MapSet.new([@campaign]),
      folders: MapSet.new([@folder]),
      mailboxes: MapSet.new([@mailbox])
    }
  }

  defp sees?(type, event),
    do: OrgChannel.scoped_event?(@member, @me, type, Map.put(event, "event_type", type))

  test "inbox content needs its mailbox, whatever campaign it belongs to" do
    assert sees?("EMAIL_RECEIVED", %{"email_account_id" => @mailbox})

    refute sees?("EMAIL_RECEIVED", %{
             "email_account_id" => @other_mailbox,
             "campaign_id" => @campaign
           })

    refute sees?("INBOX_UPDATED", %{})
  end

  test "campaign activity needs its campaign or its sending mailbox" do
    assert sees?("EMAIL_OPENED", %{
             "campaign_id" => @campaign,
             "email_account_id" => @other_mailbox
           })

    assert sees?("EMAIL_SENT", %{"campaign_id" => @other_campaign, "email_account_id" => @mailbox})

    refute sees?("EMAIL_CLICKED", %{
             "campaign_id" => @other_campaign,
             "email_account_id" => @other_mailbox
           })

    refute sees?("CAMPAIGN_UPDATED", %{"org_id" => "anything"})
  end

  test "audit refreshes only for granted entities and the member themselves" do
    audit = fn type, id -> %{"entity_type" => type, "entity_id" => id} end
    assert sees?("AUDIT_CREATED", audit.("campaign", @campaign))
    refute sees?("AUDIT_CREATED", audit.("campaign", @other_campaign))
    assert sees?("AUDIT_CREATED", audit.("folder", @folder))
    assert sees?("AUDIT_CREATED", audit.("email_account", @mailbox))
    assert sees?("AUDIT_CREATED", audit.("organization_member", @me))
    refute sees?("AUDIT_CREATED", audit.("organization_member", "someone-else"))
    refute sees?("AUDIT_CREATED", audit.("contact", @campaign))
  end

  test "events with no resource are dropped" do
    refute sees?("CUSTOM_EVENT", %{})
    refute sees?("MEETING_CREATED", %{})
  end

  test "a workspace member reaches everything" do
    assert Auth.in_scope?(%{scope: nil}, :campaigns, @other_campaign)
    refute Auth.in_scope?(@member, :mailboxes, nil)
    assert Auth.in_scope?(@member, :campaigns, String.upcase(@campaign))
  end

  test "campaign and folder edits re-read a restricted member's scope" do
    assert OrgChannel.affects_scope?(%{"entity_type" => "campaign"})
    assert OrgChannel.affects_scope?(%{"entity_type" => "folder"})
    refute OrgChannel.affects_scope?(%{"entity_type" => "contact"})
  end
end
