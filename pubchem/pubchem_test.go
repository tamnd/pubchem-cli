package pubchem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// noRateClient builds a test client pointed at srv with pacing disabled.
func noRateClient(baseURL string) *Client {
	c := NewClient()
	c.Rate = 0
	c.BaseURL = baseURL
	return c
}

// --- TestGet: basic request sends User-Agent and returns body ---

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := noRateClient(srv.URL)
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

// --- TestRetryOn503: client backs off and retries transient 503 ---

func TestRetryOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := noRateClient(srv.URL)
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

// --- TestGetCompound: decodes property table for a known compound ---

func TestGetCompound(t *testing.T) {
	const response = `{
		"PropertyTable": {
			"Properties": [{
				"CID": 2244,
				"IUPACName": "2-acetyloxybenzoic acid",
				"MolecularFormula": "C9H8O4",
				"MolecularWeight": "180.16",
				"InChI": "InChI=1S/C9H8O4/c1-6(10)13-8-5-3-2-4-7(8)9(11)12/h2-5H,1H3,(H,11,12)",
				"InChIKey": "BSYNRYMUTXBXSQ-UHFFFAOYSA-N",
				"CanonicalSMILES": "CC(=O)OC1=CC=CC=C1C(=O)O",
				"IsomericSMILES": "CC(=O)OC1=CC=CC=C1C(=O)O",
				"XLogP": 1.2,
				"TPSA": 63.6,
				"HBondDonorCount": 1,
				"HBondAcceptorCount": 4,
				"RotatableBondCount": 3,
				"Complexity": 212
			}]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer srv.Close()

	c := noRateClient(srv.URL)
	comp, err := c.GetCompound(context.Background(), 2244)
	if err != nil {
		t.Fatal(err)
	}
	if comp.CID != 2244 {
		t.Errorf("CID = %d, want 2244", comp.CID)
	}
	if comp.Formula != "C9H8O4" {
		t.Errorf("Formula = %q, want C9H8O4", comp.Formula)
	}
	if comp.IUPACName != "2-acetyloxybenzoic acid" {
		t.Errorf("IUPACName = %q", comp.IUPACName)
	}
	if comp.XLogP != 1.2 {
		t.Errorf("XLogP = %v, want 1.2", comp.XLogP)
	}
	if comp.HBondDonors != 1 {
		t.Errorf("HBondDonors = %d, want 1", comp.HBondDonors)
	}
	if comp.URL == "" {
		t.Error("URL is empty")
	}
}

// --- TestGetCompoundByName: decodes property table by name ---

func TestGetCompoundByName(t *testing.T) {
	const response = `{
		"PropertyTable": {
			"Properties": [{
				"CID": 2244,
				"IUPACName": "2-acetyloxybenzoic acid",
				"MolecularFormula": "C9H8O4",
				"MolecularWeight": "180.16",
				"InChI": "InChI=1S/C9H8O4",
				"InChIKey": "BSYNRYMUTXBXSQ-UHFFFAOYSA-N",
				"CanonicalSMILES": "CC(=O)OC1=CC=CC=C1C(=O)O",
				"IsomericSMILES": "CC(=O)OC1=CC=CC=C1C(=O)O"
			}]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer srv.Close()

	c := noRateClient(srv.URL)
	comp, err := c.GetCompoundByName(context.Background(), "aspirin")
	if err != nil {
		t.Fatal(err)
	}
	if comp.CID != 2244 {
		t.Errorf("CID = %d, want 2244", comp.CID)
	}
}

// --- TestGetSynonyms: decodes synonym list ---

func TestGetSynonyms(t *testing.T) {
	const response = `{
		"InformationList": {
			"Information": [{
				"CID": 2244,
				"Synonym": ["aspirin", "acetylsalicylic acid", "2-acetyloxybenzoic acid"]
			}]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer srv.Close()

	c := noRateClient(srv.URL)
	syns, err := c.GetSynonyms(context.Background(), 2244)
	if err != nil {
		t.Fatal(err)
	}
	if syns.CID != 2244 {
		t.Errorf("CID = %d, want 2244", syns.CID)
	}
	if len(syns.Synonyms) != 3 {
		t.Errorf("len(Synonyms) = %d, want 3", len(syns.Synonyms))
	}
	if syns.Synonyms[0] != "aspirin" {
		t.Errorf("Synonyms[0] = %q, want aspirin", syns.Synonyms[0])
	}
}

// --- TestSearchCompounds: autocomplete + property fetch + dedup by CID ---

func TestSearchCompounds(t *testing.T) {
	// The autocomplete response lists two names; both map to the same CID.
	// The result should be deduplicated to one entry.
	acResp := `{"dictionary_terms":{"compound":["aspirin","aspirin-d4"]}}`
	propResp := `{
		"PropertyTable": {
			"Properties": [{
				"CID": 2244,
				"IUPACName": "2-acetyloxybenzoic acid",
				"MolecularFormula": "C9H8O4",
				"MolecularWeight": "180.16",
				"InChI": "InChI=1S/C9H8O4",
				"InChIKey": "BSYNRYMUTXBXSQ-UHFFFAOYSA-N",
				"CanonicalSMILES": "CC(=O)OC1=CC=CC=C1C(=O)O",
				"IsomericSMILES": "CC(=O)OC1=CC=CC=C1C(=O)O"
			}]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// autocomplete endpoint lives under /compound/<name>/JSON
		// property endpoint lives under /compound/name/<name>/property/.../JSON
		path := r.URL.Path
		if len(path) > 0 && path[0] != '/' {
			path = "/" + path
		}
		// Serve autocomplete JSON when the path doesn't contain "/name/"
		// (autocomplete hits /compound/<query>/JSON, property hits /compound/name/<name>/...)
		if contains(path, "/name/") {
			_, _ = w.Write([]byte(propResp))
		} else {
			_, _ = w.Write([]byte(acResp))
		}
	}))
	defer srv.Close()

	c := noRateClient(srv.URL)
	results, err := searchCompoundsFromURL(ctx_bg(), c, srv.URL, "aspirin", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].CID != 2244 {
		t.Errorf("results[0].CID = %d, want 2244", results[0].CID)
	}
	// dedup: CID 2244 must appear only once
	count := 0
	for _, r := range results {
		if r.CID == 2244 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("CID 2244 appears %d times, want 1 (dedup failed)", count)
	}
}

// --- TestParseCID ---

func TestParseCID(t *testing.T) {
	cases := []struct {
		in  string
		cid int
		ok  bool
	}{
		{"2244", 2244, true},
		{"0", 0, false},
		{"-1", 0, false},
		{"aspirin", 0, false},
		{"", 0, false},
		{"999999999", 999999999, true},
	}
	for _, tc := range cases {
		got, ok := ParseCID(tc.in)
		if ok != tc.ok || got != tc.cid {
			t.Errorf("ParseCID(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.cid, tc.ok)
		}
	}
}

// --- helpers for tests ---

func ctx_bg() context.Context { return context.Background() }

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

// searchCompoundsFromURL is the testable form of SearchCompounds that takes an
// explicit autocomplete base URL so the test server can intercept both calls.
func searchCompoundsFromURL(ctx context.Context, c *Client, acBase, query string, limit int) ([]*Compound, error) {
	if limit <= 0 {
		limit = 10
	}

	acURL := fmt.Sprintf("%s/compound/%s/JSON?limit=%d", acBase, query, limit)
	body, err := c.Get(ctx, acURL)
	if err != nil {
		return nil, err
	}
	var ac wireAutocomplete
	if err := json.Unmarshal(body, &ac); err != nil {
		return nil, fmt.Errorf("decode autocomplete: %w", err)
	}
	names := ac.DictionaryTerms.Compound
	if len(names) == 0 {
		return nil, nil
	}

	seen := map[int]bool{}
	var out []*Compound
	for _, name := range names {
		if len(out) >= limit {
			break
		}
		u := fmt.Sprintf("%s/compound/name/%s/property/%s/JSON", c.BaseURL, name, allProps)
		b, err := c.Get(ctx, u)
		if err != nil {
			continue
		}
		var w wirePropertyTable
		if err := json.Unmarshal(b, &w); err != nil {
			continue
		}
		for _, prop := range w.PropertyTable.Properties {
			if seen[prop.CID] {
				continue
			}
			seen[prop.CID] = true
			out = append(out, prop.toCompound())
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
