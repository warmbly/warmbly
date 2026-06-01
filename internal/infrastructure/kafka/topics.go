package kafka

// Topic names use "." as the separator. It is the only separator legal in
// both Kafka topic names ("." / "_" / "-" / alphanumerics) and NATS subjects
// (which are dot-delimited), so the same name works on either event bus. Do
// not switch back to ":" — Kafka cannot host a topic whose name contains a
// colon, and auto-create silently refuses the illegal name.
func GetWorkerTopic(workerID string) string {
	return "w." + workerID
}

const (
	TopicWorkerEvents = "jobs.worker-events"
)
