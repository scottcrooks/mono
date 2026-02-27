package output

import (
	"io"
	"os"
	"strings"
)

// Mode controls how terminal output is rendered.
type Mode string

const (
	ModeInteractive Mode = "interactive"
	ModePlain       Mode = "plain"
)

func DetectMode(out io.Writer) Mode {
	return detectMode(os.Getenv, isTTY(out))
}

func detectMode(getenv func(string) string, tty bool) Mode {
	if strings.TrimSpace(getenv("NO_COLOR")) != "" {
		return ModePlain
	}
	if strings.TrimSpace(getenv("FORCE_COLOR")) != "" {
		return ModeInteractive
	}
	if !tty {
		return ModePlain
	}
	if strings.EqualFold(strings.TrimSpace(getenv("TERM")), "dumb") {
		return ModePlain
	}
	return ModeInteractive
}

func isTTY(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
