package models

// PoolLinkExecutor says which side runs a linked mailbox's warmup. Cloud runs
// every mailbox; instance marks a legacy row whose credential Cloud never received.
type PoolLinkExecutor string

const (
	PoolLinkExecutorCloud    PoolLinkExecutor = "cloud"
	PoolLinkExecutorInstance PoolLinkExecutor = "instance"
)

// Valid reports an executor this build knows; empty reads as cloud.
func (e PoolLinkExecutor) Valid() bool {
	return e == "" || e == PoolLinkExecutorCloud || e == PoolLinkExecutorInstance
}

// OrCloud resolves the empty value older clients and rows carry.
func (e PoolLinkExecutor) OrCloud() PoolLinkExecutor {
	if e == "" {
		return PoolLinkExecutorCloud
	}
	return e
}
