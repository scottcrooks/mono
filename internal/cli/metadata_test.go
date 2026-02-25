package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCollectMetadataWithoutGit(t *testing.T) {
	restore := snapshotMetadataGlobals()
	t.Cleanup(restore)

	metadataNow = func() time.Time {
		return time.Date(2026, 2, 21, 13, 14, 15, 0, time.FixedZone("PST", -8*60*60))
	}
	metadataLookPath = func(string) (string, error) {
		return "", fmt.Errorf("not found")
	}

	got := collectMetadata()
	if got.DateTimeTZ != "2026-02-21 13:14:15 PST" {
		t.Fatalf("unexpected DateTimeTZ: %q", got.DateTimeTZ)
	}
	if got.FilenameTS != "2026-02-21_13-14-15" {
		t.Fatalf("unexpected FilenameTS: %q", got.FilenameTS)
	}
	if got.RepoName != "" || got.GitBranch != "" || got.GitCommit != "" {
		t.Fatalf("expected empty git metadata, got %+v", got)
	}
}

func TestCollectMetadataWithGitFallbackBranch(t *testing.T) {
	restore := snapshotMetadataGlobals()
	t.Cleanup(restore)

	metadataNow = func() time.Time {
		return time.Date(2026, 2, 21, 16, 0, 1, 0, time.FixedZone("EST", -5*60*60))
	}
	metadataLookPath = func(string) (string, error) {
		return "/usr/bin/git", nil
	}
	metadataRunGit = func(args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		switch cmd {
		case "rev-parse --is-inside-work-tree":
			return "true", nil
		case "rev-parse --show-toplevel":
			return "/home/scott/src/argus", nil
		case "branch --show-current":
			return "", nil
		case "rev-parse --abbrev-ref HEAD":
			return "main", nil
		case "rev-parse HEAD":
			return "abc123", nil
		default:
			return "", fmt.Errorf("unexpected command: %s", cmd)
		}
	}

	got := collectMetadata()
	if got.RepoName != "argus" {
		t.Fatalf("unexpected RepoName: %q", got.RepoName)
	}
	if got.GitBranch != "main" {
		t.Fatalf("unexpected GitBranch: %q", got.GitBranch)
	}
	if got.GitCommit != "abc123" {
		t.Fatalf("unexpected GitCommit: %q", got.GitCommit)
	}
}

func TestFormatMetadataOutputOrderAndLabels(t *testing.T) {
	out := formatMetadataOutput(metadataInfo{
		DateTimeTZ: "2026-02-21 16:00:01 EST",
		GitCommit:  "abc123",
		GitBranch:  "main",
		RepoName:   "argus",
		FilenameTS: "2026-02-21_16-00-01",
	})

	want := "" +
		"Current Date/Time (TZ): 2026-02-21 16:00:01 EST\n" +
		"Current Git Commit Hash: abc123\n" +
		"Current Branch Name: main\n" +
		"Repository Name: argus\n" +
		"Timestamp For Filename: 2026-02-21_16-00-01\n"

	if out != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatMetadataOutputOmitsEmptyGitLines(t *testing.T) {
	out := formatMetadataOutput(metadataInfo{
		DateTimeTZ: "2026-02-21 16:00:01 EST",
		FilenameTS: "2026-02-21_16-00-01",
	})

	if strings.Contains(out, "Current Git Commit Hash:") {
		t.Fatal("did not expect commit line")
	}
	if strings.Contains(out, "Current Branch Name:") {
		t.Fatal("did not expect branch line")
	}
	if strings.Contains(out, "Repository Name:") {
		t.Fatal("did not expect repository line")
	}
}

func snapshotMetadataGlobals() func() {
	prevNow := metadataNow
	prevLookPath := metadataLookPath
	prevRunGit := metadataRunGit
	return func() {
		metadataNow = prevNow
		metadataLookPath = prevLookPath
		metadataRunGit = prevRunGit
	}
}
