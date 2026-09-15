package ses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SES tenants — reputation isolation between customers.
//
// THE PROBLEM THIS SOLVES. All identities live in the same AWS account, and reputation belongs to
// the ACCOUNT: one customer's bounces and complaints degrade it for everyone, and AWS throttles or
// suspends the whole account. A customer with a purchased list can stop mail for everybody else.
//
// A tenant is a logical container with its OWN metrics and sending state. When a tenant's
// reputation degrades, AWS pauses that tenant — not the account. That is the difference between
// losing one customer and losing the service.
//
// HOW IT IS WIRED. Creating the tenant and attaching the domain identity to it is not enough: the
// send itself has to say which tenant it belongs to. Over the API that is a parameter; over SMTP —
// which is our case, since Stalwart delivers to SES over SMTP — it is the `X-SES-TENANT` header,
// added by a system Sieve script at the DATA stage, not by this package.

// TenantName converts a domain into the corresponding SES tenant name.
//
// SES only accepts letters, digits, hyphen, and underscore (up to 64) — the domain's dot is out.
// The transformation is deterministic in both directions, on purpose: whoever looks at the AWS
// console has to be able to tell which customer that tenant belongs to without consulting our
// database.
func TenantName(domain string) string {
	n := strings.ToLower(strings.TrimSpace(domain))
	n = strings.NewReplacer(".", "-", "@", "-", "/", "-").Replace(n)
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

// CreateTenant creates the tenant. Idempotent: already existing counts as success, because
// provisioning repeats.
func (c *Client) CreateTenant(ctx context.Context, name string) error {
	body, _ := json.Marshal(map[string]any{
		"TenantName": name,
		// The tenant's OWN suppression list. Without this, an address that bounces for one customer
		// stays suppressed for all the others — the account's suppression list is shared, and one
		// customer must not be able to stop another from writing to a legitimate recipient.
		"SuppressionAttributes": map[string]any{
			"SuppressionScope":  "TENANT",
			"SuppressedReasons": []string{"BOUNCE", "COMPLAINT"},
		},
	})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/tenants", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusCreated {
		return nil
	}
	if alreadyExists(status, out) {
		return nil
	}
	return fmt.Errorf("ses create tenant %q: HTTP %d: %s", name, status, out)
}

// AssociateIdentity links the domain identity to the tenant. This is what makes that domain's
// metrics and sending state get counted separately.
//
// The API requires the resource's ARN, which is built from the region and the account. The
// account identifier comes from SES_ACCOUNT_ID: asking STS for it would be one extra call and one
// extra permission to obtain a number that never changes.
func (c *Client) AssociateIdentity(ctx context.Context, tenant, domain string) error {
	if c.accountID == "" {
		return fmt.Errorf("ses: SES_ACCOUNT_ID missing — without it the identity ARN cannot be built")
	}
	arn := fmt.Sprintf("arn:aws:ses:%s:%s:identity/%s", c.region, c.accountID, domain)
	body, _ := json.Marshal(map[string]any{
		"TenantName":  tenant,
		"ResourceArn": arn,
	})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/tenants/resources", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusCreated || alreadyExists(status, out) {
		return nil
	}
	return fmt.Errorf("ses associate %q to tenant %q: HTTP %d: %s", domain, tenant, status, out)
}

// TenantSendingStatus returns the tenant's sending state: ENABLED, REINSTATED, or DISABLED.
//
// DISABLED is the signal worth watching — it means AWS paused that customer for reputation.
// Finding that out from the console is finding out too late.
func (c *Client) TenantSendingStatus(ctx context.Context, name string) (string, error) {
	// POST with the name in the BODY, not a GET with the name in the path: SES v2 uses this form for
	// GetTenant. I guessed the other one and the lookup always returned "does not exist" right after
	// creating it.
	body, _ := json.Marshal(map[string]any{"TenantName": name})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/tenants/get", body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("ses get tenant %q: HTTP %d: %s", name, status, out)
	}
	var r struct {
		Tenant struct {
			SendingStatus string `json:"SendingStatus"`
		} `json:"Tenant"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return "", fmt.Errorf("decode tenant: %w", err)
	}
	return r.Tenant.SendingStatus, nil
}

// alreadyExists recognizes the "the resource is already there" response. Provisioning always
// repeats whenever an earlier step fails, so creating something twice has to be harmless.
//
// Two forms, because SES uses both: the exception name (`AlreadyExistsException`) and a plain
// English sentence ("Tenant with name X already exists in account Y"). Matching only the first
// one made the retry command fail on everything that was already created.
func alreadyExists(status int, body []byte) bool {
	if status == http.StatusConflict {
		return true
	}
	if status != http.StatusBadRequest {
		return false
	}
	// Any 400 that says "already" is a repeat, not an error: SES uses at least three forms — the
	// exception name, "already exists", and "has already been associated". Chasing every phrase is
	// a race you lose; what matters is the category.
	return strings.Contains(strings.ToLower(string(body)), "already")
}

// --- Configuration sets -------------------------------------------------------------------------
//
// WHY THEY LIVE HERE, next to the tenants: they solve the same problem through a path that does
// NOT require touching the mail. A tenant only receives traffic if each message carries the
// `X-SES-TENANT` header — and customer messages don't pass through our code, they pass through
// Stalwart. A configuration set, by contrast, can be attached to the domain's IDENTITY, and AWS
// applies it "every time email is sent from the identity." Nothing on the mail path changes.
//
// What is gained without any header at all: per-customer reputation metrics (knowing WHOSE the
// bounces are) and the ability to suspend sending for ONE customer without touching the account.
//
// What still needs the header: AWS's AUTOMATIC pause for reputation, which belongs to the tenant
// mechanism. Until that exists, the pause is ours to do — which is what SetSendingEnabled below is
// for.

// CreateConfigurationSet creates the domain's set, with reputation metrics turned on.
//
// Without `ReputationMetricsEnabled` the set exists but measures nothing, and the SES console
// keeps giving a single number for all customers — which is exactly the problem this is meant to
// solve.
func (c *Client) CreateConfigurationSet(ctx context.Context, name string) error {
	body, _ := json.Marshal(map[string]any{
		"ConfigurationSetName": name,
		"ReputationOptions":    map[string]any{"ReputationMetricsEnabled": true},
		"SendingOptions":       map[string]any{"SendingEnabled": true},
	})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/configuration-sets", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusCreated || alreadyExists(status, out) {
		return nil
	}
	return fmt.Errorf("ses create configuration set %q: HTTP %d: %s", name, status, out)
}

// SetIdentityConfigurationSet makes the set the identity's default. This is the call that
// removes the need for a per-message header.
func (c *Client) SetIdentityConfigurationSet(ctx context.Context, domain, name string) error {
	body, _ := json.Marshal(map[string]any{"ConfigurationSetName": name})
	status, out, err := c.do(ctx, http.MethodPut,
		"/v2/email/identities/"+domain+"/configuration-set", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	return fmt.Errorf("ses set default config set for %q: HTTP %d: %s", domain, status, out)
}

// SetSendingEnabled turns sending on or off for ONE customer, without touching the others or the
// account.
//
// This is the tool that was missing: until now, faced with a customer burning the reputation, the
// options were to do nothing or suspend the entire account.
func (c *Client) SetSendingEnabled(ctx context.Context, name string, enabled bool) error {
	body, _ := json.Marshal(map[string]any{"SendingEnabled": enabled})
	status, out, err := c.do(ctx, http.MethodPut,
		"/v2/email/configuration-sets/"+name+"/sending", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	return fmt.Errorf("ses sending=%v on %q: HTTP %d: %s", enabled, name, status, out)
}

// --- Per-customer reputation metrics --------------------------------------------------------------

// Reputation summarizes what SES measures on a configuration set — that is, on a customer.
type Reputation struct {
	Sent       float64
	Bounced    float64 // permanent + transient
	Complaints float64
}

// BounceRate and ComplaintRate as a fraction (0.05 = 5%). With no sends, zero: a customer who has
// not sent anything has no rate at all, and dividing by zero would suspend them for not existing.
func (r Reputation) BounceRate() float64 {
	if r.Sent == 0 {
		return 0
	}
	return r.Bounced / r.Sent
}

func (r Reputation) ComplaintRate() float64 {
	if r.Sent == 0 {
		return 0
	}
	return r.Complaints / r.Sent
}

// ConfigSetReputation reads the metrics for a configuration set over the given interval.
//
// This is the call that makes monitoring possible without touching the mail path: the
// CONFIGURATION_SET dimension is exactly what we attached to each domain identity.
func (c *Client) ConfigSetReputation(ctx context.Context, configSet string, from, until time.Time) (Reputation, error) {
	metrics := []struct{ id, name string }{
		{"s", "SEND"},
		{"pb", "PERMANENT_BOUNCE"},
		{"tb", "TRANSIENT_BOUNCE"},
		{"c", "COMPLAINT"},
	}
	queries := make([]map[string]any, 0, len(metrics))
	for _, m := range metrics {
		queries = append(queries, map[string]any{
			"Id":         m.id,
			"Namespace":  "VDM",
			"Metric":     m.name,
			"Dimensions": map[string]string{"CONFIGURATION_SET": configSet},
			"StartDate":  from.Unix(),
			"EndDate":    until.Unix(),
		})
	}
	body, _ := json.Marshal(map[string]any{"Queries": queries})

	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/metrics/batch", body)
	if err != nil {
		return Reputation{}, err
	}
	if status != http.StatusOK {
		return Reputation{}, fmt.Errorf("ses metrics for %q: HTTP %d: %s", configSet, status, out)
	}

	var r struct {
		Results []struct {
			ID     string    `json:"Id"`
			Values []float64 `json:"Values"`
		} `json:"Results"`
		Errors []struct {
			ID      string `json:"Id"`
			Message string `json:"Message"`
		} `json:"Errors"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return Reputation{}, fmt.Errorf("decode metrics: %w", err)
	}
	// An error in one of the queries must not pass for "zero bounces": zero is the value that makes
	// the watchdog conclude everything is fine, and that is exactly what is not known.
	if len(r.Errors) > 0 {
		return Reputation{}, fmt.Errorf("ses metrics for %q: %s", configSet, r.Errors[0].Message)
	}

	var rep Reputation
	for _, res := range r.Results {
		total := 0.0
		for _, v := range res.Values {
			total += v
		}
		switch res.ID {
		case "s":
			rep.Sent = total
		case "pb", "tb":
			rep.Bounced += total
		case "c":
			rep.Complaints = total
		}
	}
	return rep, nil
}
