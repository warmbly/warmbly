package worker

import (
	"log"

	"go.uber.org/zap/zapcore"
)

func (c *WorkerService) HandleError(entry zapcore.Entry) {
	select {
	case c.errorEvents <- entry:
	default:
		log.Println("⚠️ Warning: event channel full, dropping event:", entry.Message)
	}
}
