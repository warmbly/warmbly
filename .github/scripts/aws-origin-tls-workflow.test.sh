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
raise 'existing role with restrictive session policy required' unless credentials.length == 1 && credentials[0]['with'] == {
  'role-to-assume'=>'${{ vars.AWS_TERRAFORM_APPLY_ROLE_ARN }}', 'aws-region'=>'us-east-1',
  'inline-session-policy'=>'${{ steps.scope.outputs.policy }}'
}
scope = steps.index { |s| s['run'] == 'node .github/scripts/aws-origin-tls.cjs session-policy' }
raise 'source gate must precede credentials' unless scope && scope < steps.index(credentials[0])
probe = steps.find { |s| s['run'] == 'node .github/scripts/aws-origin-tls.cjs probe' }
raise 'probe timeout required' unless probe && probe['timeout-minutes'] == 10
cleanup = steps.last
raise 'bounded cleanup must always follow credentials, including probe failure' unless cleanup['run'] == 'node .github/scripts/aws-origin-tls.cjs finish' &&
  cleanup['if'] == "always() && steps.credentials.outcome == 'success'" && cleanup['timeout-minutes'] == 8
raise 'no secret inheritance, bypasses or public artifacts' if workflow.to_s.match?(/secrets|upload-artifact|AWS_TERRAFORM_ENABLED|AWS_TLS_RECOVERY|terraform apply|deploy-aws/)
jobs.each_value do |j|
  raise 'failures must stop execution' if j['continue-on-error']
  j.fetch('steps').each do |s|
    raise 'step failures must stop execution' if s['continue-on-error']
    raise 'checkout credentials must not persist' if s['uses'] == 'actions/checkout@v4' && s.dig('with', 'persist-credentials') != false
  end
end
puts 'Origin TLS workflow source gate, ordinary environment, queue and unconditional bounded cleanup checks passed'
RUBY
