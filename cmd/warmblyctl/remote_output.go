package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

func remoteWordSet(words string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.Fields(words) {
		out[word] = true
	}
	return out
}

// Unknown fields and free text fail closed, including newly added backend fields.
var remoteOutputFields = remoteWordSet(`
data pagination summary permissions user organization workers nodes mailboxes
commands results metrics broker partitions topics consumer_group topic events capture
generic_error_coverage generic_error_reason
sources counts health totals failing_endpoints jobs plans discounts members roles
id node_id worker_id email_id email_account_id mailbox_id user_id organization_id
campaign_id contact_id sequence_id task_id run_id scope_id provider account_status
status task_status task_type backfill_status level category event availability coverage
condition reason severity unit role service transport version protocol kind type
is_admin admin_permissions session_mfa_verified mfa_verified enabled active healthy
delivers replayed cleared reloaded reset requested revoked truncated has_message_id
has_next has_previous has_more next_cursor prev_cursor cursor limit total count attempts
max_attempts pending failed throttled backfilling stalled complete deferred
backfill_synced in_flight_stale pending_due delivered_last_24h failed_last_24h
abandoned_last_24h drops_last_7d lease_minutes consecutive_failures deliveries_last_7d
failed_last_7d measured_scopes expected_scopes affected_mailboxes affected_organizations
unknown_organization_rows evidence_age_seconds threshold_seconds partition committed
earliest latest committed_lag dropped retained_count retention_seconds max_events
under_5m under_30m past_reclaim_window reclaim_after_minutes age_seconds
observed_at checked_at evidence_at latest_evidence_at next_eligible_at window_start
window_end started_at received_at oldest_retained_at newest_retained_at created_at
updated_at dispatched_at oldest_dispatched_at next_retry_at replayed_at occurred_at
backfill_since backfill_started_at backfill_completed_at throttled_until last_synced_at
last_success_at last_failure_at last_started_at last_finished_at last_duration_ms
last_status interval_seconds http_status refresh_after expires_at last_seen_at
memory_bytes cpu_percent load capacity reserved used free mailbox_count online offline
mailboxes_count worker_count email_count campaign_count organization_count timestamp
access_token_expires_at refresh_token_expires_at email verified banned_at deleted_at
account_count health_state load_score usage live worker_live capacity_target
base_capacity health_multiplier age_multiplier effective_capacity utilization
sends_attempted_1h sends_succeeded_1h bounces_hard_1h bounces_soft_1h complaints_1h
auth_errors_1h total_emails_sent emails_sent_today emails_sent_total emails_sent_this_week
average_delivery_time_ms success_rate queue_depth active_campaigns connected_emails
warmup_emails warmup_enabled risk_band risk_evaluated_at warmup_health blocked_until
memory_mb goroutines uptime_seconds cpu_scope memory_scope memory_used_mb memory_limit_mb
resident_mb pinned_version desired_version enrolled_at assigned_at released_at
subscription_id run_count error_count run_requested_at next_run_at completed_at
requests_last_7d revoked_at last_used_at backends settings risk state warmup_status
auth_state send_lifecycle in_campaign daily_usage score warmup_pool_type pool_type
email_verified registration_enabled support ops analyst super view_users view_workers
view_analytics view_campaigns view_organizations manage_workers manage_settings
view_audit_logs view_warmup_pool name description bit value super_admin
`)

var remoteSafeEnums = remoteWordSet(`
fresh stale unavailable partial complete unknown healthy pending approved denied
active disabled banned suspended connected disconnected failed error running idle ok
sent sending scheduled queued delivered cancelled canceled paused stopped finished
replayed throttled backfilling stalled deferred reserved completed incomplete
info warn control_plane provider sync_policy compatibility worker_runtime
sync_control_plane_held mailbox_provider_unreachable mailbox_provider_auth_failed
mailbox_provider_throttled mailbox_provider_sync_completed sync_folder_skipped
sync_skipped_folder_search_failed sync_message_deferred sync_message_deferred_by_policy
sync_legacy_arrival worker_error log_credential_unavailable capture_not_observed
cache_unavailable no_recent_capture no_recent_evidence allowlisted_evidence_only
unhooked_runtime_sources
dependency_missing resource_absent permission_denied timeout collection_failed unsupported
committed_lag members worker_events seconds count bytes percent messages lag mailboxes
no_evidence recent_failure persistent_failure recovery_unverified safety_hold backlog
in_flight waiting_schedule waiting_retry budget daily_limit stale_telemetry
worker consumer backend smtp log gmail google outlook microsoft imap oauth password smtp_imap
watch quarantined quarantine blocked clean risky online offline loading passing failing
process container host SEND_EMAIL ADD_EMAIL REMOVE_EMAIL SYNC_EMAIL UPDATE_EMAIL
sync warmup campaign follow_up campaign_send warmup_send inbox_sync community private
super support ops analyst VIEW_USERS VIEW_WORKERS VIEW_ANALYTICS VIEW_CAMPAIGNS
VIEW_ORGANIZATIONS MANAGE_WORKERS MANAGE_SETTINGS VIEW_AUDIT_LOGS GRANT_ADMIN_ACCESS
VIEW_WARMUP_POOL STOP_CAMPAIGNS BAN_USERS MANAGE_RATE_LIMITS MANAGE_ORGANIZATIONS
MANAGE_TESTERS MANAGE_WARMUP_BANS REVIEW_APPEALS public dedicated shared global
`)

var remoteSemver = regexp.MustCompile(`^v?[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}(-[a-z0-9.-]{1,30})?$`)

func decodeRemoteJSON(payload []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 64 {
			return nil, remoteFailure(2, "invalid_json", "JSON nesting exceeds 64 levels.")
		}
		t, err := dec.Token()
		if err != nil {
			return nil, remoteFailure(2, "invalid_json", "Malformed JSON. No raw input was printed.")
		}
		if delimiter, ok := t.(json.Delim); ok {
			switch delimiter {
			case '{':
				out := map[string]any{}
				for dec.More() {
					key, err := dec.Token()
					if err != nil {
						return nil, remoteFailure(2, "invalid_json", "Malformed JSON object.")
					}
					name, ok := key.(string)
					if !ok {
						return nil, remoteFailure(2, "invalid_json", "Malformed JSON object key.")
					}
					if _, duplicate := out[name]; duplicate {
						return nil, remoteFailure(2, "invalid_json", "Duplicate JSON object keys are not accepted.")
					}
					out[name], err = read(depth + 1)
					if err != nil {
						return nil, err
					}
				}
				if _, err := dec.Token(); err != nil {
					return nil, remoteFailure(2, "invalid_json", "Unterminated JSON object.")
				}
				return out, nil
			case '[':
				out := []any{}
				for dec.More() {
					value, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					out = append(out, value)
				}
				if _, err := dec.Token(); err != nil {
					return nil, remoteFailure(2, "invalid_json", "Unterminated JSON array.")
				}
				return out, nil
			default:
				return nil, remoteFailure(2, "invalid_json", "Unexpected JSON delimiter.")
			}
		}
		return t, nil
	}
	value, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, remoteFailure(2, "invalid_json", "Expected exactly one JSON value.")
	}
	return value, nil
}

func safeRemoteString(key, value string) any {
	if value == "" {
		return ""
	}
	if (key == "cursor" || key == "next_cursor" || key == "prev_cursor") && len(value) <= 512 {
		if _, err := paging.DecodeOffsetCursor(value); err == nil {
			return value
		}
		if _, err := paging.DecodeUUID(value); err == nil {
			return value
		}
		if _, _, err := paging.DecodeTimeCursor(value); err == nil {
			return value
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return value
	}
	if id, err := uuid.Parse(value); err == nil && id.String() == strings.ToLower(value) {
		return value
	}
	if remoteSafeEnums[value] {
		return value
	}
	if key == "version" && remoteSemver.MatchString(value) {
		return value
	}
	if key == "topic" || key == "topics" || key == "consumer_group" {
		if value == "jobs.worker-events" || value == "consumer-group" {
			return value
		}
		for _, prefix := range []string{"w.", "worker-"} {
			if suffix, found := strings.CutPrefix(value, prefix); found {
				if id, err := uuid.Parse(suffix); err == nil && id.String() == suffix {
					return value
				}
			}
		}
	}
	return "[REDACTED]"
}

func redactRemoteJSON(key string, value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		redacted := 0
		for name, item := range v {
			if !remoteOutputFields[name] {
				redacted++
				continue
			}
			out[name] = redactRemoteJSON(name, item)
		}
		if redacted > 0 {
			out["_redacted_fields"] = redacted
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactRemoteJSON(key, item)
		}
		return out
	case string:
		return safeRemoteString(key, v)
	default:
		return value
	}
}

func printRemoteResponse(payload json.RawMessage) error {
	value, err := decodeRemoteJSON(payload)
	if err != nil {
		return remoteFailure(5, "invalid_response", "Response JSON is malformed, ambiguous or too deeply nested. No raw response was printed.")
	}
	return writeRemoteJSON(os.Stdout, redactRemoteJSON("", value))
}
