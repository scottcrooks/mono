package runtimecmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func writeDevEnv(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDevEnvironmentLayers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, root, service string
		inherited, want     []string
	}{
		{name: "neither", want: []string{}},
		{name: "root only", root: "ROOT=default", want: []string{"ROOT=default"}},
		{name: "service only", service: "SERVICE=own", want: []string{"SERVICE=own"}},
		{name: "precedence and duplicates", root: "A=root\nB=root\nC=root\nD=root\nG=root", service: "A=service\nB=service\nC=\nE=first\nE=last\nG=service", inherited: []string{"A=first", "A=inherited", "B=", "F=a=b", "invalid"}, want: []string{"A=inherited", "B=", "C=", "D=root", "E=last", "F=a=b", "G=service"}},
		{name: "syntax and file local references", root: "export BASE=root\nROOT_REF=${BASE}\nQUOTED=\"hello # world\"\nCOMMENT=plain # comment\nMULTI=\"first\nsecond\"\nESCAPED=\"first\\nsecond\"\nLITERAL='$(echo unused)'", service: "BASE=service\nLOCAL=${BASE}\nCROSS=${ROOT_REF}\nPARENT=${INHERITED}", inherited: []string{"INHERITED=parent"}, want: []string{"BASE=service", "COMMENT=plain", "CROSS=", "ESCAPED=first\nsecond", "INHERITED=parent", "LITERAL=$(echo unused)", "LOCAL=service", "MULTI=first\nsecond", "PARENT=", "QUOTED=hello # world", "ROOT_REF=root"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			service := filepath.Join(t.TempDir(), "absolute-service")
			if tt.root != "" {
				writeDevEnv(t, root, tt.root)
			}
			if tt.service != "" {
				writeDevEnv(t, service, tt.service)
			}
			before := append([]string(nil), tt.inherited...)
			got, err := devEnvironment(root, service, tt.inherited)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if !reflect.DeepEqual(before, tt.inherited) {
				t.Fatal("inherited input changed")
			}
		})
	}
}

func TestDevEnvironmentRootIsService(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDevEnv(t, root, "A=one\nA=${A}two")
	got, err := devEnvironment(root, filepath.Join(root, "unused", ".."), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"A=onetwo"}) {
		t.Fatalf("unexpected environment %q", got)
	}
}

func TestDevEnvironmentIsolation(t *testing.T) {
	// This test changes a process variable, so it must not run in parallel.
	const key = "MONO_DOTENV_ISOLATION_FIXTURE"
	t.Setenv(key, "parent")
	before := os.Environ()
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	writeDevEnv(t, root, "ROOT=default\n"+key+"=file")
	writeDevEnv(t, a, "ONLY_A=yes\nSERVICE=a")
	writeDevEnv(t, b, "ONLY_B=yes\nSERVICE=b")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		for _, service := range []struct{ dir, name string }{{a, "a"}, {b, "b"}} {
			wg.Go(func() {
				inherited := []string{"EXPLICIT=marker"}
				got, err := devEnvironment(root, service.dir, inherited)
				if err != nil {
					t.Error(err)
					return
				}
				own, other := "ONLY_A=yes", "ONLY_B=yes"
				if service.name == "b" {
					own, other = other, own
				}
				joined := strings.Join(got, "\n")
				for _, want := range []string{own, "ROOT=default", "SERVICE=" + service.name, "EXPLICIT=marker", key + "=file"} {
					if !strings.Contains(joined, want) {
						t.Errorf("missing %s in %q", want, got)
					}
				}
				if strings.Contains(joined, other) {
					t.Errorf("sibling leaked into %q", got)
				}
				if !reflect.DeepEqual(inherited, []string{"EXPLICIT=marker"}) {
					t.Error("input mutated")
				}
			})
		}
	}
	wg.Wait()
	if !reflect.DeepEqual(before, os.Environ()) {
		t.Fatal("parent environment changed")
	}
}

func TestDevEnvironmentErrors(t *testing.T) {
	t.Parallel()
	for _, layer := range []string{"root", "service"} {
		for _, kind := range []string{"read", "parse"} {
			t.Run(layer+"/"+kind, func(t *testing.T) {
				t.Parallel()
				root, service := t.TempDir(), t.TempDir()
				dir := root
				if layer == "service" {
					dir = service
				}
				path := filepath.Join(dir, ".env")
				const secret = "SENTINEL_SECRET_DO_NOT_DISCLOSE"
				if kind == "read" {
					if err := os.Mkdir(path, 0o755); err != nil {
						t.Fatal(err)
					}
				} else {
					writeDevEnv(t, dir, "BAD!KEY="+secret)
				}
				got, err := devEnvironment(root, service, nil)
				if err == nil {
					t.Fatal("expected error")
				}
				if got != nil {
					t.Fatalf("partial environment returned: %q", got)
				}
				if !strings.Contains(err.Error(), path) {
					t.Fatalf("missing path: %v", err)
				}
				if strings.Contains(err.Error(), secret) {
					t.Fatal("secret leaked")
				}
			})
		}
	}
}
