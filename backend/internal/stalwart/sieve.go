package stalwart

import (
	"context"
	"encoding/json"
	"fmt"
)

// System script in the DATA stage — used to add the header that identifies the customer to the
// outbound relay (X-SES-TENANT).
//
// WHY HERE AND NOT IN OUR OWN CODE: customer mail goes from the customer to Stalwart and from
// Stalwart to the relay. It never passes through our API at any point, so the header has to be
// added by whoever is in the path — and Stalwart lets us do that with a Sieve script in the DATA
// stage.
//
// WHY IT'S SAFE: if the script doesn't match anything, it adds no header and the message goes
// through anyway. What's lost is that message's isolation, not the message itself. A relay of
// ours in the middle, if it failed, would stop mail entirely — which is why this approach was
// preferred instead.

// ScriptTenantSES returns the Sieve script that adds the tenant header.
//
// The tenant name is the domain with hyphens instead of dots (see ses.TenantName), because SES
// doesn't accept dots. Sieve has no text substitution, but it has CAPTURES: each `*` in the
// pattern holds a chunk, and they're joined back together with hyphens. The patterns are tried
// from most specific to least, because `*` is greedy — with `a.b.c`, the two-chunk pattern would
// give `a.b` and `c`.
//
// It goes up to five levels. A domain with more than that would end up with the wrong name, and
// that's why the last branch doesn't exist instead of guessing: with no match, there's no header,
// and the message still goes out — the failure is visible in metrics instead of silently baked
// into the name.
func ScriptTenantSES() string {
	return `require ["editheader", "envelope", "variables"];

# Identifies the customer to the outbound relay. The name is the sender's domain with hyphens
# instead of dots — see internal/ses.TenantName, which must keep producing exactly the same thing.
if envelope :domain :matches "from" "*.*.*.*.*" {
    addheader :last "X-SES-TENANT" "${1}-${2}-${3}-${4}-${5}";
} elsif envelope :domain :matches "from" "*.*.*.*" {
    addheader :last "X-SES-TENANT" "${1}-${2}-${3}-${4}";
} elsif envelope :domain :matches "from" "*.*.*" {
    addheader :last "X-SES-TENANT" "${1}-${2}-${3}";
} elsif envelope :domain :matches "from" "*.*" {
    addheader :last "X-SES-TENANT" "${1}-${2}";
}
`
}

// UpsertSieveScript creates or updates a system script and returns its id.
func (c *Client) UpsertSieveScript(ctx context.Context, name, content string) (string, error) {
	// Look up first: the registry's `set` has no "create or update", and provisioning twice must
	// not leave two scripts with the same name.
	id, err := c.sieveScriptID(ctx, name)
	if err != nil {
		return "", err
	}

	fields := map[string]any{"name": name, "contents": content, "isActive": true}
	args := map[string]any{}
	if id != "" {
		args["update"] = map[string]any{id: fields}
	} else {
		args["create"] = map[string]any{"s1": fields}
	}

	raw, err := c.call(ctx, "x:SieveSystemScript/set", args)
	if err != nil {
		return "", err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode SieveSystemScript/set: %w", err)
	}
	if len(r.NotCreated) > 0 || len(r.NotUpdated) > 0 {
		return "", fmt.Errorf("stalwart rejected the script: created=%s updated=%s",
			mustJSON(r.NotCreated), mustJSON(r.NotUpdated))
	}
	if id != "" {
		return id, nil
	}
	return firstCreatedID(&r)
}

// sieveScriptID looks up a system script by name. Returns "" if it doesn't exist.
func (c *Client) sieveScriptID(ctx context.Context, name string) (string, error) {
	raw, err := c.call(ctx, "x:SieveSystemScript/query", map[string]any{})
	if err != nil {
		return "", err
	}
	var q struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		return "", fmt.Errorf("decode SieveSystemScript/query: %w", err)
	}
	if len(q.IDs) == 0 {
		return "", nil
	}
	raw, err = c.call(ctx, "x:SieveSystemScript/get", map[string]any{"ids": q.IDs})
	if err != nil {
		return "", err
	}
	var g struct {
		List []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return "", fmt.Errorf("decode SieveSystemScript/get: %w", err)
	}
	for _, s := range g.List {
		if s.Name == name {
			return s.ID, nil
		}
	}
	return "", nil
}

// GetDataStageScript returns the expression that currently selects the DATA stage script, exactly
// as-is — so it can be restored if something goes wrong.
func (c *Client) GetDataStageScript(ctx context.Context) (json.RawMessage, error) {
	raw, err := c.call(ctx, "x:MtaStageData/get", map[string]any{"ids": []string{singletonID}})
	if err != nil {
		return nil, err
	}
	var r struct {
		List []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || len(r.List) == 0 {
		return nil, fmt.Errorf("MtaStageData/get returned nothing: %v", err)
	}
	return r.List[0], nil
}

// SetDataStageScript makes the DATA stage run the given script.
func (c *Client) SetDataStageScript(ctx context.Context, scriptName string) error {
	// PATCH PATHS, not a nested object. Stalwart's JMAP updates by pointer (`script/match/0/if`);
	// sending `script` as an object with a `match` inside it is rejected with `invalidPatch`
	// pointing precisely at `script/match`.
	//
	// SUBMISSION PORTS ONLY. The DATA stage runs on ALL sessions, including mail that COMES IN on
	// port 25 — and there `env_from.domain` is the domain of whoever wrote to us. Without this
	// condition, a message from Gmail would arrive with an `X-SES-TENANT` header built from
	// Gmail's own domain tacked on: garbage in customer mail and a wrong number in the relay's
	// metrics.
	raw, err := c.call(ctx, "x:MtaStageData/set", map[string]any{
		"update": map[string]any{
			singletonID: map[string]any{
				"script/else":         "''",
				"script/match/0/if":   "local_port == 465 || local_port == 587",
				"script/match/0/then": fmt.Sprintf("'%s'", scriptName),
			},
		},
	})
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode MtaStageData/set: %w", err)
	}
	if len(r.NotUpdated) > 0 {
		return fmt.Errorf("stalwart rejected the DATA stage: %s", mustJSON(r.NotUpdated))
	}
	return nil
}
