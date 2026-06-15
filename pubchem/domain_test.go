package pubchem

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve). No network calls.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "pubchem" {
		t.Errorf("Scheme = %q, want pubchem", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "pubchem" {
		t.Errorf("Identity.Binary = %q, want pubchem", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct{ in, typ, id string }{
		{"2244", "compound", "2244"},
		{"aspirin", "compound", "aspirin"},
		{"https://pubchem.ncbi.nlm.nih.gov/compound/2244", "compound", "2244"},
		{"https://pubchem.ncbi.nlm.nih.gov/compound/aspirin", "compound", "aspirin"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestLocate(t *testing.T) {
	cases := []struct {
		uriType string
		id      string
		want    string
	}{
		{"compound", "2244", "https://pubchem.ncbi.nlm.nih.gov/compound/2244"},
		{"compound", "aspirin", "https://pubchem.ncbi.nlm.nih.gov/compound/aspirin"},
		{"synonyms", "2244", "https://pubchem.ncbi.nlm.nih.gov/compound/2244#section=Synonyms"},
	}
	for _, tc := range cases {
		got, err := Domain{}.Locate(tc.uriType, tc.id)
		if err != nil || got != tc.want {
			t.Errorf("Locate(%q, %q) = (%q, %v), want (%q, nil)",
				tc.uriType, tc.id, got, err, tc.want)
		}
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "foo")
	if err == nil {
		t.Error("expected error for unknown uriType, got nil")
	}
}

// TestHostWiring mounts the driver in a kit Host and checks the round trip:
// a Compound mints to its URI, resolve finds it again.
func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	c := &Compound{CID: 2244, IUPACName: "2-acetyloxybenzoic acid", Formula: "C9H8O4"}
	u, err := h.Mint(c)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := "pubchem://compound/2244"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("pubchem", "2244")
	if err != nil || got.String() != "pubchem://compound/2244" {
		t.Errorf("ResolveOn = (%q, %v), want pubchem://compound/2244", got.String(), err)
	}
}
