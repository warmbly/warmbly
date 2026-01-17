package kafka

func GetWorkerTopic(workerID string) string {
	return "w:" + workerID
}

const (
	TopicWorkerEvents = "jobs:worker-events"
)
