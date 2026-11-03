package alsaseq

import "testing"

func TestPush3DefaultsUntilFound(t *testing.T) {
	var p Push3
	if got := p.Addr(); got != (Addr{16, 0}) {
		t.Fatalf("default = %+v", got)
	}
	if !p.Is(Addr{16, 2}) || p.Is(Addr{20, 0}) {
		t.Fatal("Is before Refresh")
	}
}

func TestPush3FollowsShiftedClient(t *testing.T) {
	var p Push3
	p.refreshFrom(parseFixture(t, "testdata/seq_clients_bare.txt"))
	if got := p.Addr(); got != (Addr{16, 0}) {
		t.Fatalf("bare = %+v", got)
	}
	p.refreshFrom(parseFixture(t, "testdata/seq_clients_usb_attached.txt"))
	got := p.Addr()
	if got.Port != 0 || got.Client == 16 {
		t.Fatalf("shifted = %+v, want a client other than 16", got)
	}
	if !p.Is(Addr{got.Client, 1}) || p.Is(Addr{16, 0}) {
		t.Fatal("Is after shift")
	}
	if !p.IsLivePort(got) || p.IsLivePort(Addr{got.Client, 1}) {
		t.Fatal("IsLivePort")
	}
}

func TestPush3KeepsLastWhenNotListed(t *testing.T) {
	var p Push3
	p.refreshFrom(parseFixture(t, "testdata/seq_clients_usb_attached.txt"))
	want := p.Addr()
	p.refreshFrom(nil)
	if p.Addr() != want {
		t.Fatalf("lost the address: %+v", p.Addr())
	}
}
