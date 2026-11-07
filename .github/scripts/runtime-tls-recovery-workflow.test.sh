#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
ruby -ryaml <<'RUBY'
workflow = YAML.load_file('.github/workflows/runtime-tls-recovery.yml')
jobs = workflow.fetch('jobs')
plan, apply, redeploy, verify = jobs.values_at('plan', 'apply', 'redeploy', 'verify')
raise 'plan must be manual and depend on offline checks' unless plan['if'] == "github.event_name == 'workflow_dispatch'" && plan['needs'] == 'check'
raise 'plan must use the read-only environment' unless plan['environment'] == 'aws-infrastructure-plan'
entry = plan.fetch('steps').first
raise 'explicit enablement and new main run are required' unless entry.fetch('run').include?('[ "$RECOVERY_ENABLED" = true ]') && entry.fetch('run').include?('[ "$GITHUB_RUN_ATTEMPT" = 1 ]') && entry.fetch('run').include?('[ "$GITHUB_REF" = refs/heads/main ]')
raise 'apply must follow plan approval' unless apply['needs'] == 'plan' && apply['environment'] == 'aws-infrastructure-approval'
raise 'apply must compare the exact plan approval digest' unless apply['steps'].any? { |s| s.dig('env', 'APPROVED_DIGEST') == '${{ needs.plan.outputs.digest }}' }
[apply, verify].each do |job|
  raise 'production serialization must be retained' unless job.dig('concurrency', 'group') == 'aws-production' && job.dig('concurrency', 'cancel-in-progress') == false
end
raise 'redeploy must follow both plan and apply' unless redeploy['needs'].sort == ['apply', 'plan']
raise 'redeploy must use the normal local workflow' unless redeploy['uses'] == './.github/workflows/deploy-aws.yml'
expected = {'tag'=>'${{ needs.plan.outputs.release }}', 'mode'=>'redeploy', 'services'=>'all', 'fleet'=>false, 'require_attestation'=>true, 'bake_seconds'=>'300'}
raise 'same release, all services, provenance and full bakes are required without fleet or secret-drop overrides' unless redeploy['with'] == expected
raise 'final verification must follow redeploy' unless verify['needs'] == 'redeploy' && verify['environment'] == 'aws-infrastructure-plan'
jobs.each_value do |job|
  raise 'recovery cannot ignore job failures' if job['continue-on-error']
  (job['steps'] || []).each { |step| raise 'recovery cannot ignore step failures' if step['continue-on-error'] }
end
deploy = YAML.load_file('.github/workflows/deploy-aws.yml')
call = (deploy['on'] || deploy[true]).fetch('workflow_call')
raise 'existing deployment defaults must be retained' unless call.dig('inputs', 'bake_seconds', 'default') == ''
raise 'dropped-secret allowance remains disabled by default' if call.fetch('inputs').key?('allow_dropped_secrets')
puts 'Recovery workflow approval, sequencing, deployment and default compatibility checks passed'
RUBY
