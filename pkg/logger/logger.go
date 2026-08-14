package logger

import (
	"log"
	"strings"
)

const (
	reset   = "\033[0m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
)

// level is the numeric threshold: a message is emitted when its own level is
// >= the configured threshold.
type level int

const (
	levelDebug level = iota
	levelInfo
	levelWarn
	levelError
)

// currentLevel is the package-level threshold. The zero value (info) is the
// default: Info/Warn/Error all print, preserving the pre-filtering behavior
// until SetLevel is called.
var currentLevel = levelInfo

// SetLevel configures the package-wide threshold. Values: debug, info, warn,
// error (case-insensitive). Any unknown value defaults to info.
func SetLevel(s string) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		currentLevel = levelDebug
	case "warn":
		currentLevel = levelWarn
	case "error":
		currentLevel = levelError
	default:
		currentLevel = levelInfo
	}
}

func Info(format string, args ...any) {
	if currentLevel > levelInfo {
		return
	}
	log.Printf(green+"[INFO] "+format+reset, args...)
}

func Error(format string, args ...any) {
	log.Printf(red+"[ERROR] "+format+reset, args...)
}

func Warn(format string, args ...any) {
	if currentLevel > levelWarn {
		return
	}
	log.Printf(yellow+"[WARN] "+format+reset, args...)
}
