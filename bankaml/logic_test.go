package bankaml

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/checkmarble/marble-backend/models"
	"github.com/google/uuid"
)

func TestScoreKeepsSanctionsSeparate(t *testing.T) {
	score := CalculateScore(ScoreInput{
		Client: ClientInput{ClientRef: "c1", KYCScore: floatPtr(10), Transactions: []Transaction{{Amount: 10}}, OpenAlerts: 0},
		Matches: []Match{{Qualification: QualConfirmed, EntityID: "os-1"}},
		Formula: DefaultFormula(),
		Reason:  "UC12",
	})
	if score.Total > 20 {
		t.Fatalf("low inputs produced total %v", score.Total)
	}
	if !score.ReviewRequired || score.SanctionsStatus != "potential_sanctions_match" {
		t.Fatalf("confirmed screening match was neutralized: %+v", score)
	}
	if err := ValidateFormula(DefaultFormula()); err != nil {
		t.Fatal(err)
	}
}

func TestScenariosPositiveNegativeAndBoundary(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	active := DefaultScenarios()
	for i := range active {
		active[i].Status = "active"
		active[i].Enabled = true
	}
	only := func(code string) []Scenario {
		for _, scenario := range active {
			if scenario.Code == code {
				return []Scenario{scenario}
			}
		}
		t.Fatal(code)
		return nil
	}
	if alerts := EvaluateScenarios(only("UC01"), Population{Now: now, Transactions: []Transaction{
		{ClientID: "c", AccountID: "a", Amount: 4000, At: now.Add(-3 * time.Hour)},
		{ClientID: "c", AccountID: "a", Amount: 4100, At: now.Add(-2 * time.Hour)},
		{ClientID: "c", AccountID: "a", Amount: 3900, At: now.Add(-time.Hour)},
	}}); len(alerts) != 1 {
		t.Fatalf("UC01 positive: %d", len(alerts))
	}
	if alerts := EvaluateScenarios(only("UC01"), Population{Now: now, Transactions: []Transaction{
		{ClientID: "c", AccountID: "a", Amount: 4000, At: now},
		{ClientID: "c", AccountID: "a", Amount: 4000, At: now},
	}}); len(alerts) != 0 {
		t.Fatalf("UC01 below count: %d", len(alerts))
	}
	if alerts := EvaluateScenarios(only("UC07"), Population{Transactions: []Transaction{{ID: "t", ClientID: "c", Country: "FR"}}}); len(alerts) != 0 {
		t.Fatal("UC07 negative")
	}
	if alerts := EvaluateScenarios(only("UC07"), Population{Transactions: []Transaction{{ID: "t", ClientID: "c", Country: "IR"}}}); len(alerts) != 1 {
		t.Fatal("UC07 positive")
	}
	if alerts := EvaluateScenarios(only("UC03"), Population{Transactions: []Transaction{
		{ClientID: "c", Direction: "in", Amount: 1000, At: now, Counterparty: "x"},
		{ClientID: "c", Direction: "out", Amount: 900, At: now.Add(time.Hour), Counterparty: "y"},
	}}); len(alerts) != 1 {
		t.Fatal("UC03 positive")
	}
	if alerts := EvaluateScenarios(only("UC08"), Population{Transactions: []Transaction{
		{ClientID: "c", Amount: 50, At: now.Add(-200 * 24 * time.Hour)},
		{ClientID: "c", Amount: 5000, At: now},
	}}); len(alerts) != 1 {
		t.Fatal("UC08 positive")
	}
	disabled := only("UC07")
	disabled[0].Enabled = false
	if alerts := EvaluateScenarios(disabled, Population{Transactions: []Transaction{{ClientID: "c", Country: "IR"}}}); len(alerts) != 0 {
		t.Fatal("disabled scenario fired")
	}
}

func TestWorkflowFourEyes(t *testing.T) {
	item := Case{Status: CaseNew, Investigator: ""}
	next, err := ApplyTransition(TransitionInput{To: CaseInvestigating, Actor: "analyst", Case: item})
	if err != nil {
		t.Fatal(err)
	}
	next, err = ApplyTransition(TransitionInput{To: CaseValidated, Actor: "analyst", Reason: "ok", Case: next})
	if err == nil {
		t.Fatal("investigator validated their own case")
	}
	next = Case{Status: CaseInvestigating, Investigator: "analyst"}
	next, err = ApplyTransition(TransitionInput{To: CaseValidated, Actor: "reviewer", Reason: "ok", Case: next})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyTransition(TransitionInput{To: CaseClosed, Actor: "reviewer", Reason: "done", Case: next}); err == nil {
		t.Fatal("reviewer closed their own validation")
	}
	if _, err := ApplyTransition(TransitionInput{To: CaseClosed, Actor: "other", Case: next}); err == nil {
		t.Fatal("closure without reason")
	}
}

func TestPermissions(t *testing.T) {
	viewer := models.Credentials{Roles: []models.Role{models.VIEWER}}
	analyst := models.Credentials{Roles: []models.Role{models.ANALYST}}
	if Allowed(viewer, ActSearch) || Allowed(viewer, ActBatch) {
		t.Fatal("viewer can act")
	}
	if !Allowed(analyst, ActSearch) || !Allowed(analyst, ActQualify) || Allowed(analyst, ActBatch) || Allowed(analyst, ActClose) {
		t.Fatal("analyst permissions")
	}
}

func TestCompareDoesNotTreatMissingAsDelisting(t *testing.T) {
	score := 80.0
	diff := CompareMatches(
		[]Match{{EntityID: "a", EntityName: "A", Score: &score, Datasets: []string{"list"}}},
		nil,
	)
	if len(diff.Missing) != 1 || diff.Missing[0].Change != "missing" || diff.Note == "" {
		t.Fatalf("%+v", diff)
	}
}

func TestOpenSanctionsParsingAndRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("authorization") == "" {
			t.Error("missing authorization")
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 || json.Valid(body) == false {
			t.Error("empty body")
		}
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"responses":{"q":{"results":[{"id":"NK-1","caption":"Ada Lovelace","schema":"Person","datasets":["us_ofac_sdn"],"score":0.91,"features":{"name_part_match":0.9},"properties":{"name":["Ada Lovelace"],"birthDate":["1815"]},"match":true}]}}}`))
	}))
	defer server.Close()
	client := &OpenSanctions{Host: server.URL, APIKey: "secret", Client: server.Client(), MinGap: time.Millisecond, Retries: 2}
	matches, err := client.Match(context.Background(), SearchQuery{Name: "Ada Lovelace", BirthDate: "1815", EntityType: "Person"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(matches) != 1 || matches[0].EntityID != "NK-1" || matches[0].Datasets[0] != "us_ofac_sdn" {
		t.Fatalf("calls %d matches %+v", calls, matches)
	}
	if matches[0].Score == nil || *matches[0].Score < 90 {
		t.Fatalf("score %+v", matches[0].Score)
	}
	sent := Transmitted(SearchQuery{Name: "Ada", BirthDate: "1815", EntityType: "Person"})
	if len(sent.Fields) != 2 {
		t.Fatalf("transmitted %+v", sent)
	}
}

func TestQualifyRequiresReasonAndRole(t *testing.T) {
	svc := &Service{Repo: NewMemory(), Sanctions: &OpenSanctions{}, Now: func() time.Time { return time.Now().UTC() }}
	org := uuid.New()
	viewer := models.Credentials{OrganizationId: org, Roles: []models.Role{models.VIEWER}, ActorIdentity: models.Identity{Email: "view@example"}}
	if _, _, _, err := svc.Search(context.Background(), viewer, SearchQuery{Name: "Ada"}); err == nil {
		t.Fatal("viewer searched")
	}
	analyst := models.Credentials{OrganizationId: org, Roles: []models.Role{models.ANALYST}, ActorIdentity: models.Identity{Email: "ana@example"}}
	match := Match{ID: "m1", OrgID: org.String(), Qualification: QualToReview}
	_ = svc.Repo.InsertSearch(context.Background(), Search{ID: "s1", OrgID: org.String()}, []Match{match})
	if _, err := svc.Qualify(context.Background(), analyst, "m1", QualConfirmed, ""); err == nil {
		t.Fatal("confirmation without reason")
	}
	saved, err := svc.Qualify(context.Background(), analyst, "m1", QualConfirmed, "documents match")
	if err != nil || saved.QualifiedBy != "ana@example" {
		t.Fatal(err, saved.QualifiedBy)
	}
}

func floatPtr(v float64) *float64 { return &v }
