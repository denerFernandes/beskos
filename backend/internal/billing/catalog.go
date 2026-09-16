package billing

const DefaultPlanID = "trial"

// plans is the FALLBACK catalog (the source of truth is the `plans` table; see planFromRow).
// Feature tiers: base currency is USD, and price goes up with the feature set. The Enterprise
// tier ("contact us") is not listed here — it is landing-page marketing only, no self-serve checkout.
var plans = []Plan{
	{ID: "trial", Name: "Trial", MailboxRange: "up to 3", MaxMailboxes: 3, MaxStorageBytes: 5 * GB, MaxDailySend: 200, MaxMeetParticipants: 5, PriceCentsPerUser: 0,
		Recording: "none", RecordingDays: 30, OnlyOfficeEdit: true, WikiPublicLinks: true, IsActive: true},
	{ID: "base", Name: "Base", MailboxRange: "1–10", MaxMailboxes: 10, MaxStorageBytes: 30 * GB, MaxDailySend: 1000, MaxMeetParticipants: 25, PriceCentsPerUser: 700,
		Prices:   map[string]int{"EUR": 699, "GBP": 599, "BRL": 3900, "PLN": 2900},
		MinUsers: 1, Recording: "offline", RecordingDays: 30, OnlyOfficeEdit: true, WikiPublicLinks: true, IsActive: true},
	{ID: "business", Name: "Business", MailboxRange: "1–50", MaxMailboxes: 50, MaxStorageBytes: 200 * GB, MaxDailySend: 5000, MaxMeetParticipants: 100, PriceCentsPerUser: 1200,
		Prices:   map[string]int{"EUR": 1199, "GBP": 999, "BRL": 6900, "PLN": 4900},
		MinUsers: 1, Recording: "offline", RecordingDays: 30, OnlyOfficeEdit: true, WikiPublicLinks: true, IsActive: true},
	{ID: "pro", Name: "Pro", MailboxRange: "1–200", MaxMailboxes: 200, MaxStorageBytes: 1024 * GB, MaxDailySend: 20000, MaxMeetParticipants: 200, PriceCentsPerUser: 2000,
		Prices:   map[string]int{"EUR": 1899, "GBP": 1699, "BRL": 10900, "PLN": 7900},
		MinUsers: 1, Recording: "offline_priority", RecordingDays: 30, OnlyOfficeEdit: true, WikiPublicLinks: true, IsActive: true},
}
