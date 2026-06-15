package pubchem

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes PubChem as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/pubchem-cli/pubchem"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// pubchem:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone pubchem binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the PubChem driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "pubchem",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "pubchem",
			Short:  "Read public PubChem compound data",
			Long: `Read public PubChem compound data over plain HTTPS.

pubchem fetches chemical compound records from the PubChem REST API
(pubchem.ncbi.nlm.nih.gov), shapes them into clean records, and prints
output that pipes into the rest of your tools. No API key, no setup.`,
			Site: Host,
			Repo: "https://github.com/tamnd/pubchem-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// compound: fetch one compound by name or CID
	kit.Handle(app, kit.OpMeta{
		Name: "compound", Group: "read", Single: true, URIType: "compound", Resolver: true,
		Summary: "Fetch a compound by name or CID",
		Args:    []kit.Arg{{Name: "ref", Help: "compound name or numeric CID"}},
	}, getCompound)

	// search: autocomplete + property fetch for matching compounds
	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "read",
		Summary: "Search for compounds by name",
		Args:    []kit.Arg{{Name: "query", Help: "search terms"}},
	}, searchCompounds)

	// synonyms: list known synonyms for a compound
	kit.Handle(app, kit.OpMeta{
		Name: "synonyms", Group: "read", Single: true, URIType: "synonyms",
		Summary: "List synonyms for a compound by name or CID",
		Args:    []kit.Arg{{Name: "ref", Help: "compound name or numeric CID"}},
	}, getSynonyms)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- input structs ---

type compoundRef struct {
	Ref    string  `kit:"arg" help:"compound name or numeric CID"`
	Client *Client `kit:"inject"`
}

type searchInput struct {
	Query  string  `kit:"arg" help:"search terms"`
	Limit  int     `kit:"flag,inherit" help:"max results (default 10)"`
	Client *Client `kit:"inject"`
}

type synonymsRef struct {
	Ref    string  `kit:"arg" help:"compound name or numeric CID"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func getCompound(ctx context.Context, in compoundRef, emit func(*Compound) error) error {
	c, err := fetchCompound(ctx, in.Client, in.Ref)
	if err != nil {
		return mapErr(err)
	}
	return emit(c)
}

func searchCompounds(ctx context.Context, in searchInput, emit func(*Compound) error) error {
	results, err := in.Client.SearchCompounds(ctx, in.Query, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, c := range results {
		if err := emit(c); err != nil {
			return err
		}
	}
	return nil
}

func getSynonyms(ctx context.Context, in synonymsRef, emit func(*Synonyms) error) error {
	var (
		s   *Synonyms
		err error
	)
	if cid, ok := ParseCID(in.Ref); ok {
		s, err = in.Client.GetSynonyms(ctx, cid)
	} else {
		s, err = in.Client.GetSynonymsByName(ctx, in.Ref)
	}
	if err != nil {
		return mapErr(err)
	}
	return emit(s)
}

// fetchCompound dispatches to GetCompound or GetCompoundByName based on ref.
func fetchCompound(ctx context.Context, c *Client, ref string) (*Compound, error) {
	if cid, ok := ParseCID(ref); ok {
		return c.GetCompound(ctx, cid)
	}
	return c.GetCompoundByName(ctx, ref)
}

// --- Resolver: pure string functions, no network ---

// Classify turns any accepted input into a canonical (uriType, id).
// Numeric inputs are CIDs; anything else is treated as a compound name.
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	// strip a pasted https URL down to the path segment
	if u, err2 := url.Parse(input); err2 == nil &&
		(u.Scheme == "http" || u.Scheme == "https") &&
		strings.Contains(u.Host, "pubchem") {
		// e.g. https://pubchem.ncbi.nlm.nih.gov/compound/2244
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 {
			input = parts[len(parts)-1]
		}
	}
	if input == "" {
		return "", "", errs.Usage("empty PubChem reference")
	}
	if _, ok := ParseCID(input); ok {
		return "compound", input, nil
	}
	return "compound", input, nil
}

// Locate returns the live https URL for a (uriType, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "compound":
		if cid, ok := ParseCID(id); ok {
			return fmt.Sprintf("https://pubchem.ncbi.nlm.nih.gov/compound/%d", cid), nil
		}
		return fmt.Sprintf("https://pubchem.ncbi.nlm.nih.gov/compound/%s",
			url.PathEscape(id)), nil
	case "synonyms":
		if cid, ok := ParseCID(id); ok {
			return fmt.Sprintf("https://pubchem.ncbi.nlm.nih.gov/compound/%d#section=Synonyms", cid), nil
		}
		return fmt.Sprintf("https://pubchem.ncbi.nlm.nih.gov/compound/%s#section=Synonyms",
			url.PathEscape(id)), nil
	default:
		return "", errs.Usage("pubchem has no resource type %q", uriType)
	}
}

// mapErr converts a library error into the appropriate kit error kind.
func mapErr(err error) error {
	return err
}
