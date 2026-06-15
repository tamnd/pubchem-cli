// Package pubchem is the library behind the pubchem command line:
// the HTTP client, request shaping, and the typed data models for the
// PubChem REST API (pubchem.ncbi.nlm.nih.gov).
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite (<=5 req/s),
// and retries transient failures (429 and 5xx) that the public API throws
// under load. All domain methods build their URLs and decode JSON on top of it.
package pubchem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultUserAgent identifies the client to PubChem.
const DefaultUserAgent = "pubchem-cli/dev (+https://github.com/tamnd/pubchem-cli)"

// Host is the canonical hostname for PubChem.
const Host = "pubchem.ncbi.nlm.nih.gov"

// BaseURL is the PubChem PUG REST root.
const BaseURL = "https://pubchem.ncbi.nlm.nih.gov/rest/pug"

// AutocompleteURL is the root for the autocomplete endpoint (not under /rest/pug).
const AutocompleteURL = "https://pubchem.ncbi.nlm.nih.gov/rest/autocomplete"

// allProps is the property list fetched for every compound.
const allProps = "IUPACName,MolecularFormula,MolecularWeight,InChI,InChIKey,CanonicalSMILES,IsomericSMILES,XLogP,TPSA,HBondDonorCount,HBondAcceptorCount,RotatableBondCount,Complexity"

// --- Config ---

// Config holds the tunables for a Client. Zero values fall back to the defaults
// in DefaultConfig.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration // min gap between requests; 200ms = 5 req/s cap
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns the configuration PubChem recommends.
func DefaultConfig() Config {
	return Config{
		BaseURL:   BaseURL,
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Timeout:   15 * time.Second,
		Retries:   3,
	}
}

// --- Client ---

// Client talks to the PubChem REST API over HTTPS.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	BaseURL   string
	Rate      time.Duration
	Retries   int

	last time.Time
}

// NewClient returns a Client with DefaultConfig settings.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		BaseURL:   cfg.BaseURL,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// NewClientFromConfig builds a Client from a Config, falling back to
// DefaultConfig for zero fields.
func NewClientFromConfig(cfg Config) *Client {
	def := DefaultConfig()
	if cfg.BaseURL == "" {
		cfg.BaseURL = def.BaseURL
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = def.UserAgent
	}
	if cfg.Rate == 0 {
		cfg.Rate = def.Rate
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.Retries == 0 {
		cfg.Retries = def.Retries
	}
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		BaseURL:   cfg.BaseURL,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// Get fetches the given URL and returns the body. It paces and retries on
// transient errors (429, 5xx). The body is fully read and closed.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace sleeps until at least Rate has elapsed since the last request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- Public output types ---

// Compound is a normalized PubChem compound record.
type Compound struct {
	CID             int     `json:"cid" kit:"id"`
	IUPACName       string  `json:"iupac_name"`
	Formula         string  `json:"formula"`
	Weight          string  `json:"weight"`
	InChI           string  `json:"inchi"`
	InChIKey        string  `json:"inchikey"`
	SMILES          string  `json:"smiles"`
	XLogP           float64 `json:"xlogp,omitempty"`
	TPSA            float64 `json:"tpsa,omitempty"`
	HBondDonors     int     `json:"hbond_donors,omitempty"`
	HBondAcceptors  int     `json:"hbond_acceptors,omitempty"`
	RotatableBonds  int     `json:"rotatable_bonds,omitempty"`
	Complexity      float64 `json:"complexity,omitempty"`
	URL             string  `json:"url"`
}

// Synonyms is a list of known names for one compound.
type Synonyms struct {
	CID      int      `json:"cid" kit:"id"`
	Synonyms []string `json:"synonyms"`
}

// --- wire types for JSON decoding ---

type wirePropertyTable struct {
	PropertyTable struct {
		Properties []wireProperty `json:"Properties"`
	} `json:"PropertyTable"`
}

type wireProperty struct {
	CID              int     `json:"CID"`
	IUPACName        string  `json:"IUPACName"`
	MolecularFormula string  `json:"MolecularFormula"`
	MolecularWeight  string  `json:"MolecularWeight"`
	InChI            string  `json:"InChI"`
	InChIKey         string  `json:"InChIKey"`
	CanonicalSMILES  string  `json:"CanonicalSMILES"`
	IsomericSMILES   string  `json:"IsomericSMILES"`
	XLogP            float64 `json:"XLogP"`
	TPSA             float64 `json:"TPSA"`
	HBondDonorCount  int     `json:"HBondDonorCount"`
	HBondAcceptorCount int   `json:"HBondAcceptorCount"`
	RotatableBondCount int   `json:"RotatableBondCount"`
	Complexity       float64 `json:"Complexity"`
}

func (p wireProperty) toCompound() *Compound {
	smiles := p.IsomericSMILES
	if smiles == "" {
		smiles = p.CanonicalSMILES
	}
	return &Compound{
		CID:            p.CID,
		IUPACName:      p.IUPACName,
		Formula:        p.MolecularFormula,
		Weight:         p.MolecularWeight,
		InChI:          p.InChI,
		InChIKey:       p.InChIKey,
		SMILES:         smiles,
		XLogP:          p.XLogP,
		TPSA:           p.TPSA,
		HBondDonors:    p.HBondDonorCount,
		HBondAcceptors: p.HBondAcceptorCount,
		RotatableBonds: p.RotatableBondCount,
		Complexity:     p.Complexity,
		URL:            fmt.Sprintf("https://pubchem.ncbi.nlm.nih.gov/compound/%d", p.CID),
	}
}

type wireSynonymList struct {
	InformationList struct {
		Information []struct {
			CID     int      `json:"CID"`
			Synonym []string `json:"Synonym"`
		} `json:"Information"`
	} `json:"InformationList"`
}

type wireAutocomplete struct {
	DictionaryTerms struct {
		Compound []string `json:"compound"`
	} `json:"dictionary_terms"`
}

// --- Domain methods ---

// GetCompound fetches a compound by CID and returns its normalized record.
func (c *Client) GetCompound(ctx context.Context, cid int) (*Compound, error) {
	u := fmt.Sprintf("%s/compound/cid/%d/property/%s/JSON", c.BaseURL, cid, allProps)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var w wirePropertyTable
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("decode compound %d: %w", cid, err)
	}
	if len(w.PropertyTable.Properties) == 0 {
		return nil, fmt.Errorf("compound %d: not found", cid)
	}
	return w.PropertyTable.Properties[0].toCompound(), nil
}

// GetCompoundByName fetches the first compound matching name.
func (c *Client) GetCompoundByName(ctx context.Context, name string) (*Compound, error) {
	u := fmt.Sprintf("%s/compound/name/%s/property/%s/JSON",
		c.BaseURL, url.PathEscape(name), allProps)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var w wirePropertyTable
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("decode compound %q: %w", name, err)
	}
	if len(w.PropertyTable.Properties) == 0 {
		return nil, fmt.Errorf("compound %q: not found", name)
	}
	return w.PropertyTable.Properties[0].toCompound(), nil
}

// SearchCompounds autocompletes query, then fetches properties for each
// suggested name, deduplicating by CID. At most limit compounds are returned
// (limit <= 0 means 10).
func (c *Client) SearchCompounds(ctx context.Context, query string, limit int) ([]*Compound, error) {
	if limit <= 0 {
		limit = 10
	}
	// 1. autocomplete for name suggestions
	acURL := fmt.Sprintf("%s/compound/%s/JSON?limit=%d",
		AutocompleteURL, url.PathEscape(query), limit)
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

	// 2. fetch properties for each suggestion, dedup by CID
	seen := map[int]bool{}
	var out []*Compound
	for _, name := range names {
		if len(out) >= limit {
			break
		}
		u := fmt.Sprintf("%s/compound/name/%s/property/%s/JSON",
			c.BaseURL, url.PathEscape(name), allProps)
		b, err := c.Get(ctx, u)
		if err != nil {
			continue // name may not match, skip
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

// GetSynonyms fetches all known synonyms for a compound by CID.
func (c *Client) GetSynonyms(ctx context.Context, cid int) (*Synonyms, error) {
	u := fmt.Sprintf("%s/compound/cid/%d/synonyms/JSON", c.BaseURL, cid)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var w wireSynonymList
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("decode synonyms %d: %w", cid, err)
	}
	if len(w.InformationList.Information) == 0 {
		return nil, fmt.Errorf("synonyms for CID %d: not found", cid)
	}
	info := w.InformationList.Information[0]
	return &Synonyms{CID: info.CID, Synonyms: info.Synonym}, nil
}

// GetSynonymsByName fetches synonyms for a compound by name, first resolving
// the name to a CID via properties.
func (c *Client) GetSynonymsByName(ctx context.Context, name string) (*Synonyms, error) {
	comp, err := c.GetCompoundByName(ctx, name)
	if err != nil {
		return nil, err
	}
	return c.GetSynonyms(ctx, comp.CID)
}

// ParseCID tries to parse s as a CID integer. Returns (cid, true) on success.
func ParseCID(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
