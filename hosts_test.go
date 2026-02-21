package main

import "testing"

func TestServiceHostEntries(t *testing.T) {
	t.Parallel()

	entries, err := serviceHostEntries([]Service{
		{Name: "pythia"},
		{Name: "mallos"},
	})
	if err != nil {
		t.Fatalf("serviceHostEntries returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0] != "127.0.0.1 pythia.argus.local" {
		t.Fatalf("unexpected entry[0]: %q", entries[0])
	}
	if entries[1] != "127.0.0.1 mallos.argus.local" {
		t.Fatalf("unexpected entry[1]: %q", entries[1])
	}
}

func TestServiceHostEntriesInvalidName(t *testing.T) {
	t.Parallel()

	_, err := serviceHostEntries([]Service{{Name: "bad_name"}})
	if err == nil {
		t.Fatal("expected error for invalid service name")
	}
}

func TestUpsertManagedHostsBlockAppend(t *testing.T) {
	t.Parallel()

	existing := "127.0.0.1 localhost\n"
	block := buildHostsManagedBlock([]string{"127.0.0.1 mallos.argus.local"})

	updated, changed, err := upsertManagedHostsBlock(existing, block)
	if err != nil {
		t.Fatalf("upsertManagedHostsBlock returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}

	expected := "127.0.0.1 localhost\n\n" + block
	if updated != expected {
		t.Fatalf("unexpected updated content:\n%s", updated)
	}
}

func TestUpsertManagedHostsBlockReplace(t *testing.T) {
	t.Parallel()

	existing := "127.0.0.1 localhost\n\n# BEGIN ARGUS HOSTS\n127.0.0.1 old.argus.local\n# END ARGUS HOSTS\n"
	block := buildHostsManagedBlock([]string{"127.0.0.1 pythia.argus.local"})

	updated, changed, err := upsertManagedHostsBlock(existing, block)
	if err != nil {
		t.Fatalf("upsertManagedHostsBlock returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}

	expected := "127.0.0.1 localhost\n\n" + block
	if updated != expected {
		t.Fatalf("unexpected updated content:\n%s", updated)
	}
}

func TestUpsertManagedHostsBlockIdempotent(t *testing.T) {
	t.Parallel()

	block := buildHostsManagedBlock([]string{"127.0.0.1 mallos.argus.local"})
	existing := "127.0.0.1 localhost\n\n" + block

	updated, changed, err := upsertManagedHostsBlock(existing, block)
	if err != nil {
		t.Fatalf("upsertManagedHostsBlock returned error: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false")
	}
	if updated != existing {
		t.Fatal("expected updated to match existing content")
	}
}

func TestRemoveManagedHostsBlock(t *testing.T) {
	t.Parallel()

	content := "127.0.0.1 localhost\n\n# BEGIN ARGUS HOSTS\n127.0.0.1 mallos.argus.local\n# END ARGUS HOSTS\n"

	updated, changed, err := removeManagedHostsBlock(content)
	if err != nil {
		t.Fatalf("removeManagedHostsBlock returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}

	expected := "127.0.0.1 localhost\n"
	if updated != expected {
		t.Fatalf("unexpected updated content:\n%q", updated)
	}
}

func TestRemoveManagedHostsBlockNoOp(t *testing.T) {
	t.Parallel()

	content := "127.0.0.1 localhost\n"

	updated, changed, err := removeManagedHostsBlock(content)
	if err != nil {
		t.Fatalf("removeManagedHostsBlock returned error: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false")
	}
	if updated != content {
		t.Fatal("expected updated to match input content")
	}
}

func TestRemoveManagedHostsBlockMissingEnd(t *testing.T) {
	t.Parallel()

	content := "127.0.0.1 localhost\n# BEGIN ARGUS HOSTS\n127.0.0.1 mallos.argus.local\n"

	_, _, err := removeManagedHostsBlock(content)
	if err == nil {
		t.Fatal("expected missing end marker error")
	}
}
