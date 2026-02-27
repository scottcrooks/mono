package output

type Style string

const (
	StyleInfo    Style = "info"
	StyleSuccess Style = "success"
	StyleWarn    Style = "warn"
	StyleError   Style = "error"
	StyleMuted   Style = "muted"
)

const ansiReset = "\x1b[0m"

func ApplyStyle(mode Mode, style Style, text string) string {
	if mode != ModeInteractive {
		return text
	}
	code := ""
	switch style {
	case StyleInfo:
		code = "\x1b[36m"
	case StyleSuccess:
		code = "\x1b[32m"
	case StyleWarn:
		code = "\x1b[33m"
	case StyleError:
		code = "\x1b[31m"
	case StyleMuted:
		code = "\x1b[90m"
	default:
		return text
	}
	return code + text + ansiReset
}
