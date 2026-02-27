package output

import "testing"

func TestDetectModePlainWhenNotTTY(t *testing.T) {
	mode := detectMode(func(string) string { return "" }, false)
	if mode != ModePlain {
		t.Fatalf("expected plain mode, got %s", mode)
	}
}

func TestDetectModeInteractiveWhenCISet(t *testing.T) {
	mode := detectMode(func(key string) string {
		if key == "CI" {
			return "1"
		}
		return ""
	}, true)
	if mode != ModeInteractive {
		t.Fatalf("expected interactive mode, got %s", mode)
	}
}

func TestDetectModePlainWhenNoColorSet(t *testing.T) {
	mode := detectMode(func(key string) string {
		if key == "NO_COLOR" {
			return "1"
		}
		return ""
	}, true)
	if mode != ModePlain {
		t.Fatalf("expected plain mode, got %s", mode)
	}
}

func TestDetectModeInteractiveForTTYWithoutFlags(t *testing.T) {
	mode := detectMode(func(string) string { return "" }, true)
	if mode != ModeInteractive {
		t.Fatalf("expected interactive mode, got %s", mode)
	}
}

func TestDetectModeInteractiveWhenForceColorSet(t *testing.T) {
	mode := detectMode(func(key string) string {
		if key == "FORCE_COLOR" {
			return "1"
		}
		return ""
	}, false)
	if mode != ModeInteractive {
		t.Fatalf("expected interactive mode, got %s", mode)
	}
}

func TestDetectModePlainWhenNoColorAndForceColorSet(t *testing.T) {
	mode := detectMode(func(key string) string {
		switch key {
		case "NO_COLOR":
			return "1"
		case "FORCE_COLOR":
			return "1"
		default:
			return ""
		}
	}, true)
	if mode != ModePlain {
		t.Fatalf("expected plain mode, got %s", mode)
	}
}
