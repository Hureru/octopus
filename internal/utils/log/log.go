package log

import (
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Config struct {
	Level           string
	Format          string
	Caller          bool
	StacktraceLevel string
}

var Logger *zap.SugaredLogger
var atomicLevel = zap.NewAtomicLevelAt(zap.InfoLevel)

func init() {
	Configure(Config{Level: "info", Format: "console", Caller: true, StacktraceLevel: "error"})
}

func Configure(cfg Config) {
	if strings.TrimSpace(cfg.Level) == "" {
		cfg.Level = atomicLevel.Level().String()
	}
	if strings.TrimSpace(cfg.Format) == "" {
		cfg.Format = "console"
	}
	if strings.TrimSpace(cfg.StacktraceLevel) == "" {
		cfg.StacktraceLevel = "error"
	}
	SetLevel(cfg.Level)

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		MessageKey:     "msg",
		CallerKey:      "caller",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var encoder zapcore.Encoder
	if strings.EqualFold(cfg.Format, "json") {
		encoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoderConfig.EncodeTime = shortConsoleTimeEncoder
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	core := zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), atomicLevel)
	opts := []zap.Option{zap.AddCallerSkip(1)}
	if cfg.Caller {
		opts = append(opts, zap.AddCaller())
	}
	if stackLevel, ok := parseLevel(cfg.StacktraceLevel); ok {
		opts = append(opts, zap.AddStacktrace(stackLevel))
	}
	Logger = zap.New(core, opts...).Sugar()
}

func shortConsoleTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("15:04:05.000"))
}

func parseLevel(level string) (zapcore.Level, bool) {
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(strings.TrimSpace(level))); err != nil {
		return zapcore.InfoLevel, false
	}
	return lvl, true
}

func SetLevel(level string) {
	if lvl, ok := parseLevel(level); ok {
		atomicLevel.SetLevel(lvl)
	}
}

func IsDebugEnabled() bool {
	return atomicLevel.Enabled(zapcore.DebugLevel)
}

func Infof(template string, args ...interface{}) {
	Logger.Infof(template, args...)
}

func Errorf(template string, args ...interface{}) {
	Logger.Errorf(template, args...)
}

func Warnf(template string, args ...interface{}) {
	Logger.Warnf(template, args...)
}

func Debugf(template string, args ...interface{}) {
	Logger.Debugf(template, args...)
}

// Infow / Warnw / Errorf / Debugw emit structured key-value log entries —
// the message is the event name, and keysAndValues are flattened into the
// log line as `key=value` pairs (zap SugaredLogger semantics). Prefer the
// w-suffix variants for audit / telemetry style events so downstream log
// pipelines (loki, elk, grep) can parse the fields reliably.
func Infow(msg string, keysAndValues ...interface{}) {
	Logger.Infow(msg, keysAndValues...)
}

func Warnw(msg string, keysAndValues ...interface{}) {
	Logger.Warnw(msg, keysAndValues...)
}

func Errorw(msg string, keysAndValues ...interface{}) {
	Logger.Errorw(msg, keysAndValues...)
}

func Debugw(msg string, keysAndValues ...interface{}) {
	Logger.Debugw(msg, keysAndValues...)
}
