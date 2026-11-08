package core

import (
	"reflect"
	"testing"
)

func TestWithDefaultCI(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  []string
		want []string
	}{
		{"absent", []string{"MARKER=kept"}, []string{"MARKER=kept", "CI=true"}},
		{"empty", []string{"CI=", "MARKER=kept"}, []string{"CI=", "MARKER=kept"}},
		{"false", []string{"CI=false"}, []string{"CI=false"}},
		{"true", []string{"CI=true"}, []string{"CI=true"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := WithDefaultCI(tt.env)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("WithDefaultCI(%q) = %q, want %q", tt.env, got, tt.want)
			}
			if !reflect.DeepEqual(tt.env, func() []string {
				switch tt.name {
				case "absent":
					return []string{"MARKER=kept"}
				case "empty":
					return []string{"CI=", "MARKER=kept"}
				case "false":
					return []string{"CI=false"}
				default:
					return []string{"CI=true"}
				}
			}()) {
				t.Fatal("input environment mutated")
			}
		})
	}
}
