package worker

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type LoggerClient struct {
	Events chan zapcore.Entry
	*zap.Logger
}

type CustomCore struct {
	zapcore.Core
	OnError func(entry zapcore.Entry)
}

func (c *CustomCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	if entry.Level == zapcore.ErrorLevel && c.OnError != nil {
		c.OnError(entry)
	}
	return c.Core.Write(entry, fields)
}

func NewLoggerWithHandler(onError func(zapcore.Entry)) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(cfg.EncoderConfig),
		zapcore.AddSync(os.Stdout),
		cfg.Level,
	)

	customCore := &CustomCore{Core: core, OnError: onError}

	return zap.New(customCore, zap.AddCaller()), nil
}
