package log

import (
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const InfoLevel = "info"

func timeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
}

func setHandler(l zapcore.Level) error {
	atom := zap.NewAtomicLevel()
	encoderCfg := zap.NewDevelopmentConfig()

	if l.String() == InfoLevel {
		encoderCfg.DisableCaller = true
	}

	encoderCfg.EncoderConfig.TimeKey = "time"
	encoderCfg.EncoderConfig.EncodeTime = timeEncoder
	encoderCfg.EncoderConfig.LevelKey = "level"
	encoderCfg.EncoderConfig.MessageKey = "message"
	encoderCfg.EncoderConfig.CallerKey = "caller"
	encoderCfg.Level = atom
	//encoderCfg.Encoding = "json"

	logger, err := encoderCfg.Build()
	if err != nil {
		return err
	}

	zap.ReplaceGlobals(logger)
	atom.SetLevel(l)
	return nil
}

func Verbosity(l string) error {
	switch strings.ToLower(l) {
	case "error":
		return setHandler(zap.ErrorLevel)
	case "info":
		return setHandler(zap.InfoLevel)
	case "debug":
		return setHandler(zap.DebugLevel)
	default:
		return setHandler(zap.DebugLevel)
	}
}
