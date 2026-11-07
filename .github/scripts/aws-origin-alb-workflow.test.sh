#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
ruby -ryaml <<'RUBY'
workflow = YAML.load_file('.github/workflows/configure-origin-alb-aws.yml')
trigger = workflow['on'] || workflow[true]
raise 'only manual execution and offline PR checks are supported' unless trigger.keys.sort == ['pull_request', 'workflow_dispatch']
inputs = trigger.fetch('workflow_dispatch').fetch('inputs')
raise 'plan must be the default' unless inputs.dig('mode', 'default') == 'plan' && inputs.dig('mode', 'options') == ['plan', 'apply', 'rollback']
raise 'exact plan selectors are required' unless inputs.keys.sort == ['mode', 'plan_run_id', 'plan_sha256', 'plan_version']
jobs = workflow.fetch('jobs')
raise 'no deployment or recovery jobs belong here' unless jobs.keys.sort == ['check', 'configure']
job = jobs.fetch('configure')
raise 'manual main workflow must follow offline checks' unless job['needs'] == 'check' && job['if'] == "github.event_name == 'workflow_dispatch'"
raise 'ordinary environment must be retained' unless job['environment'] == 'aws-infrastructure'
raise 'production serialization must be retained' unless job.dig('concurrency', 'group') == 'aws-production' && job.dig('concurrency', 'cancel-in-progress') == false
raise 'only contents and OIDC permissions are needed' unless job['permissions'] == {'contents'=>'read', 'id-token'=>'write'}
steps = job.fetch('steps')
credentials = steps.select { |s| s['uses'] == 'aws-actions/configure-aws-credentials@v4' }
raise 'one existing role with session restriction is required' unless credentials.length == 1 && credentials[0]['with'] == {
  'role-to-assume'=>'${{ vars.AWS_TERRAFORM_APPLY_ROLE_ARN }}', 'aws-region'=>'us-east-1',
  'inline-session-policy'=>'${{ steps.scope.outputs.policy }}'
}
scope = steps.index { |s| s['run'] == 'node .github/scripts/aws-origin-alb.cjs session-policy' }
raise 'scope validation must precede credentials' unless scope && scope < steps.index(credentials[0])
raise 'no secret inheritance or protection changes are supported' if workflow.to_s.match?(/secrets|aws-infrastructure-approval|AWS_TLS_RECOVERY|AWS_TERRAFORM_ENABLED|terraform apply|deploy-aws/)
jobs.each_value do |j|
  raise 'job failures must stop execution' if j['continue-on-error']
  j.fetch('steps').each do |s|
    raise 'step failures must stop execution' if s['continue-on-error']
    raise 'checkout credentials must not persist' if s['uses'] == 'actions/checkout@v4' && s.dig('with', 'persist-credentials') != false
  end
end
puts 'Origin ALB workflow default, ordinary environment, session scope and production queue checks passed'
RUBY
