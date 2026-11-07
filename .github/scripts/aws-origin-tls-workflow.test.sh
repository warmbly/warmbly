#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
ruby -ryaml <<'RUBY'
workflow = YAML.load_file('.github/workflows/probe-origin-tls-aws.yml')
trigger = workflow['on'] || workflow[true]
raise 'only manual execution and offline checks are supported' unless trigger.keys.sort == ['pull_request', 'workflow_dispatch']
raise 'no arbitrary execution inputs are supported' unless trigger['workflow_dispatch'].nil?
jobs = workflow.fetch('jobs')
raise 'only checks and a diagnostic belong here' unless jobs.keys.sort == ['check', 'probe']
job = jobs.fetch('probe')
raise 'manual diagnostic must follow checks' unless job['needs'] == 'check' && job['if'] == "github.event_name == 'workflow_dispatch'"
raise 'ordinary environment required' unless job['environment'] == 'aws-infrastructure'
raise 'production serialization required' unless job.dig('concurrency', 'group') == 'aws-production' && job.dig('concurrency', 'cancel-in-progress') == false
raise 'bounded job required' unless job['timeout-minutes'] == 25
raise 'only contents and OIDC permissions needed' unless job['permissions'] == {'contents'=>'read', 'id-token'=>'write'}
steps = job.fetch('steps')
credentials = steps.select { |s| s['uses'] == 'aws-actions/configure-aws-credentials@v4' }
raise 'separate probe and cleanup credentials required' unless credentials.map { |s| s['id'] } == ['credentials', 'cleanup_credentials']
['probe', 'cleanup'].each_with_index do |phase, i|
  raise 'existing role with phase policy and no credential reuse required' unless credentials[i]['with'] == {
    'role-to-assume'=>'${{ vars.AWS_TERRAFORM_APPLY_ROLE_ARN }}', 'aws-region'=>'us-east-1',
    'unset-current-credentials'=>true,
    'inline-session-policy'=>"${{ steps.scope.outputs.#{phase}_policy }}"
  }
end
scope = steps.index { |s| s['run'] == 'node .github/scripts/aws-origin-tls.cjs session-policy' }
raise 'source gate must precede credentials' unless scope && steps[scope]['id'] == 'scope' && scope < steps.index(credentials[0])
raise 'probe credentials require the successful source gate' if credentials[0]['if']
probe = steps.find { |s| s['run'] == 'node .github/scripts/aws-origin-tls.cjs probe' }
raise 'probe requires its successful credentials and timeout' unless probe && probe['timeout-minutes'] == 10 &&
  probe['if'] == "success() && steps.credentials.outcome == 'success'"
raise 'probe must run once immediately after its credentials' unless steps.count { |s| s['run'] == probe['run'] } == 1 &&
  steps.index(probe) == steps.index(credentials[0]) + 1
raise 'cleanup credentials must follow probe failure or credential failure after a successful source gate' unless
  credentials[1]['if'] == "always() && steps.scope.outcome == 'success'" && credentials[1]['timeout-minutes'] == 2 &&
  steps.index(credentials[1]) == steps.index(probe) + 1
cleanup = steps.last
raise 'bounded finalizer must require successful cleanup credentials with no earlier-session fallback' unless
  cleanup['run'] == 'node .github/scripts/aws-origin-tls.cjs finish' &&
  cleanup['if'] == "always() && steps.cleanup_credentials.outcome == 'success'" && cleanup['timeout-minutes'] == 8 &&
  steps.index(cleanup) == steps.index(credentials[1]) + 1
raise 'no secret inheritance, bypasses or public artifacts' if workflow.to_s.match?(/secrets|upload-artifact|AWS_TERRAFORM_ENABLED|AWS_TLS_RECOVERY|terraform apply|deploy-aws/)
jobs.each_value do |j|
  raise 'failures must stop execution' if j['continue-on-error']
  j.fetch('steps').each do |s|
    raise 'step failures must stop execution' if s['continue-on-error']
    raise 'checkout credentials must not persist' if s['uses'] == 'actions/checkout@v4' && s.dig('with', 'persist-credentials') != false
  end
end
puts 'Origin TLS workflow source gate, phase credentials, failure gating, ordinary environment and bounded cleanup checks passed'
RUBY
