// Package bankaml is a complementary AML module. It does not read or change
// Marble license entitlements. Screening, scoring and the investigation
// workflow implemented here are independent of the license-gated Marble features.
package bankaml

import "time"

const (
	QualToReview   = "to_review"
	QualInAnalysis = "in_analysis"
	QualDismissed  = "dismissed"
	QualConfirmed  = "confirmed"
	QualEscalate   = "escalate"
	QualError      = "error"

	CaseNew          = "new"
	CaseToQualify    = "to_qualify"
	CaseInvestigating = "investigating"
	CaseWaiting      = "waiting_info"
	CaseEscalated    = "escalated"
	CaseSecondLevel  = "second_level"
	CaseValidated    = "validated"
	CaseClosed       = "closed"

	BatchPending = "pending"
	BatchRunning = "running"
	BatchDone    = "done"
	BatchDoneErr = "done_with_errors"
	BatchFailed  = "failed"
)

type SearchQuery struct {
	ClientRef    string   `json:"client_ref,omitempty"`
	EntityType   string   `json:"entity_type"`
	Name         string   `json:"name"`
	Alias        string   `json:"alias,omitempty"`
	BirthDate    string   `json:"birth_date,omitempty"`
	Nationality  string   `json:"nationality,omitempty"`
	Country      string   `json:"country,omitempty"`
	Identifiers  []string `json:"identifiers,omitempty"`
	Threshold    int      `json:"threshold,omitempty"`
	Force        bool     `json:"force,omitempty"`
}

type Transmission struct {
	Destination string   `json:"destination"`
	Fields      []string `json:"fields"`
	Note        string   `json:"note"`
}

type Search struct {
	ID           string       `json:"id"`
	OrgID        string       `json:"org_id"`
	ClientRef    string       `json:"client_ref,omitempty"`
	Query        SearchQuery  `json:"query"`
	QueryHash    string       `json:"query_hash"`
	Source       string       `json:"source"`
	Status       string       `json:"status"`
	Error        string       `json:"error,omitempty"`
	Transmitted  Transmission `json:"transmitted"`
	CreatedBy    string       `json:"created_by"`
	CreatedAt    time.Time    `json:"created_at"`
	FromCache    bool         `json:"from_cache,omitempty"`
}

type Match struct {
	ID                  string         `json:"id"`
	OrgID               string         `json:"org_id"`
	SearchID            string         `json:"search_id"`
	ClientRef           string         `json:"client_ref,omitempty"`
	EntityID            string         `json:"entity_id"`
	EntityName          string         `json:"entity_name"`
	EntityType          string         `json:"entity_type"`
	Datasets            []string       `json:"datasets"`
	Matched             map[string]any `json:"matched"`
	Mismatched          map[string]any `json:"mismatched"`
	Score               *float64       `json:"score,omitempty"`
	Explanation         map[string]any `json:"explanation"`
	ConsultedAt         time.Time      `json:"consulted_at"`
	Qualification       string         `json:"qualification"`
	QualificationReason string         `json:"qualification_reason,omitempty"`
	QualifiedBy         string         `json:"qualified_by,omitempty"`
	QualifiedAt         *time.Time     `json:"qualified_at,omitempty"`
	Change              string         `json:"change,omitempty"`
}

type Event struct {
	ID         string         `json:"id"`
	OrgID      string         `json:"org_id"`
	ObjectType string         `json:"object_type"`
	ObjectID   string         `json:"object_id"`
	Action     string         `json:"action"`
	Actor      string         `json:"actor"`
	Reason     string         `json:"reason,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Case struct {
	ID            string         `json:"id"`
	OrgID         string         `json:"org_id"`
	AlertKey      string         `json:"alert_key"`
	ClientRef     string         `json:"client_ref"`
	Status        string         `json:"status"`
	Priority      string         `json:"priority"`
	Assignee      string         `json:"assignee,omitempty"`
	Team          string         `json:"team,omitempty"`
	DueAt         *time.Time     `json:"due_at,omitempty"`
	ScenarioCode  string         `json:"scenario_code,omitempty"`
	Alert         map[string]any `json:"alert"`
	Score         map[string]any `json:"score,omitempty"`
	Screening     map[string]any `json:"screening,omitempty"`
	ClosureReason string         `json:"closure_reason,omitempty"`
	Investigator  string         `json:"investigator,omitempty"`
	Level1Actor   string         `json:"level1_actor,omitempty"`
	Level2Actor   string         `json:"level2_actor,omitempty"`
	OpenedBy      string         `json:"opened_by"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Events        []Event        `json:"events,omitempty"`
}

type ClientInput struct {
	ClientRef   string        `json:"client_ref"`
	EntityType  string        `json:"entity_type"`
	Name        string        `json:"name"`
	Alias       string        `json:"alias,omitempty"`
	BirthDate   string        `json:"birth_date,omitempty"`
	Nationality string        `json:"nationality,omitempty"`
	Country     string        `json:"country,omitempty"`
	Identifiers []string      `json:"identifiers,omitempty"`
	Profile     ClientProfile `json:"profile"`
	Transactions []Transaction `json:"transactions,omitempty"`
	OpenAlerts  int           `json:"open_alerts,omitempty"`
	ClosedAlerts int          `json:"closed_alerts,omitempty"`
	KYCScore    *float64      `json:"kyc_score,omitempty"`
	Modified    bool          `json:"modified,omitempty"`
	LastError   bool          `json:"last_error,omitempty"`
}

type Transaction struct {
	ID                  string    `json:"id"`
	ClientID            string    `json:"client_id"`
	AccountID           string    `json:"account_id"`
	Direction           string    `json:"direction"`
	Amount              float64   `json:"amount"`
	Currency            string    `json:"currency,omitempty"`
	Country             string    `json:"country,omitempty"`
	Channel             string    `json:"channel,omitempty"`
	Counterparty        string    `json:"counterparty,omitempty"`
	CounterpartyAccount string    `json:"counterparty_account,omitempty"`
	Type                string    `json:"type,omitempty"`
	At                  time.Time `json:"at"`
}

type ClientProfile struct {
	Type                   string    `json:"type,omitempty"`
	Country                string    `json:"country,omitempty"`
	Sector                 string    `json:"sector,omitempty"`
	ExpectedMonthlyVolume  float64   `json:"expected_monthly_volume,omitempty"`
	ExpectedMonthlyCount   int       `json:"expected_monthly_count,omitempty"`
	KYCUpdatedAt           time.Time `json:"kyc_updated_at,omitempty"`
	Relationship           string    `json:"relationship,omitempty"`
}

type Batch struct {
	ID              string        `json:"id"`
	OrgID           string        `json:"org_id"`
	Mode            string        `json:"mode"`
	Status          string        `json:"status"`
	Population      []ClientInput `json:"population,omitempty"`
	ChunkSize       int           `json:"chunk_size"`
	Cursor          int           `json:"cursor"`
	Total           int           `json:"total"`
	Succeeded       int           `json:"succeeded"`
	Failed          int           `json:"failed"`
	NewMatches      int           `json:"new_matches"`
	ChangedMatches  int           `json:"changed_matches"`
	MissingMatches  int           `json:"missing_matches"`
	Remaining       int           `json:"remaining"`
	TriggeredBy     string        `json:"triggered_by"`
	Error           string        `json:"error,omitempty"`
	StartedAt       *time.Time    `json:"started_at,omitempty"`
	FinishedAt      *time.Time    `json:"finished_at,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	Items           []BatchItem   `json:"items,omitempty"`
}

type BatchItem struct {
	ID             string `json:"id"`
	BatchID        string `json:"batch_id"`
	ClientRef      string `json:"client_ref"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	NewMatches     int    `json:"new_matches"`
	ChangedMatches int    `json:"changed_matches"`
	MissingMatches int    `json:"missing_matches"`
}

type Formula struct {
	ID          string             `json:"id"`
	OrgID       string             `json:"org_id"`
	Weights     Weights            `json:"weights"`
	Classes     map[string]float64 `json:"classes"`
	Factors     []Factor           `json:"factors"`
	Description string             `json:"description"`
	Status      string             `json:"status"`
	Version     int                `json:"version"`
	Author      string             `json:"author"`
	Approver    string             `json:"approver,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
}

type Weights struct {
	KYC          float64 `json:"kyc"`
	Transactions float64 `json:"transactions"`
	Alerts       float64 `json:"alerts"`
}

type Factor struct {
	Code        string  `json:"code"`
	Component   string  `json:"component"`
	Source      string  `json:"source"`
	Rule        string  `json:"rule"`
	Weight      float64 `json:"weight"`
	Version     int     `json:"version"`
	Justification string `json:"justification"`
}

type Score struct {
	ID               string         `json:"id"`
	OrgID            string         `json:"org_id"`
	ClientRef        string         `json:"client_ref"`
	Total            float64        `json:"total"`
	Components       map[string]float64 `json:"components"`
	Factors          []FactorResult `json:"factors"`
	FormulaVersion   int            `json:"formula_version"`
	SanctionsStatus  string         `json:"sanctions_status"`
	ReviewRequired   bool           `json:"review_required"`
	Missing          []string       `json:"missing"`
	ReferenceAt      time.Time      `json:"reference_at"`
	Reason           string         `json:"reason"`
	CreatedBy        string         `json:"created_by"`
	CreatedAt        time.Time      `json:"created_at"`
}

type FactorResult struct {
	Code  string  `json:"code"`
	Value float64 `json:"value"`
	Note  string  `json:"note"`
}

type Scenario struct {
	OrgID       string         `json:"org_id"`
	Code        string         `json:"code"`
	Description string         `json:"description"`
	Params      map[string]any `json:"params"`
	Severity    string         `json:"severity"`
	Priority    string         `json:"priority"`
	Version     int            `json:"version"`
	Status      string         `json:"status"`
	Enabled     bool           `json:"enabled"`
	Author      string         `json:"author"`
	Approver    string         `json:"approver,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type Setting struct {
	ID          string         `json:"id"`
	OrgID       string         `json:"org_id"`
	Key         string         `json:"key"`
	Value       map[string]any `json:"value"`
	Description string         `json:"description"`
	Status      string         `json:"status"`
	Version     int            `json:"version"`
	EffectiveAt *time.Time     `json:"effective_at,omitempty"`
	Author      string         `json:"author"`
	Approver    string         `json:"approver,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

type Alert struct {
	Code        string         `json:"code"`
	ClientID    string         `json:"client_id"`
	Severity    string         `json:"severity"`
	Priority    string         `json:"priority"`
	DedupKey    string         `json:"dedup_key"`
	Explanation string         `json:"explanation"`
	Version     string         `json:"version"`
	Evidence    map[string]any `json:"evidence"`
}

type DiffSummary struct {
	New     []Match `json:"new"`
	Changed []Match `json:"changed"`
	Missing []Match `json:"missing"`
	Note    string  `json:"note"`
}
