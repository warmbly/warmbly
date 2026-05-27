package models

// ProvisioningJobState matches the CHECK constraint on provisioning_jobs.state.
// The state machine progresses pending -> creating_server -> creating_ips ->
// assigning_ips -> setting_rdns -> installing -> verifying -> completed.
// Any failure transitions to rolling_back -> failed.
type ProvisioningJobState string

const (
	ProvJobPending        ProvisioningJobState = "pending"
	ProvJobCreatingServer ProvisioningJobState = "creating_server"
	ProvJobCreatingIPs    ProvisioningJobState = "creating_ips"
	ProvJobAssigningIPs   ProvisioningJobState = "assigning_ips"
	ProvJobSettingRDNS    ProvisioningJobState = "setting_rdns"
	ProvJobInstalling     ProvisioningJobState = "installing"
	ProvJobVerifying      ProvisioningJobState = "verifying"
	ProvJobCompleted      ProvisioningJobState = "completed"
	ProvJobFailed         ProvisioningJobState = "failed"
	ProvJobRollingBack    ProvisioningJobState = "rolling_back"
)
