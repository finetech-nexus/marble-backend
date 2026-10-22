package bankaml

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/checkmarble/marble-backend/models"
	"github.com/google/uuid"
)

var (
	errNotFound = errors.New("not found")
	errDenied   = errors.New("forbidden")
	errConflict = errors.New("conflict")
)

type Service struct {
	Repo       Repository
	Sanctions  *OpenSanctions
	Cooldown   time.Duration
	Now        func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) require(creds models.Credentials, action Action) error {
	if !Allowed(creds, action) {
		return errDenied
	}
	return nil
}

func (s *Service) Search(ctx context.Context, creds models.Credentials, query SearchQuery) (Search, []Match, DiffSummary, error) {
	if err := s.require(creds, ActSearch); err != nil {
		return Search{}, nil, DiffSummary{}, err
	}
	if query.Name == "" {
		return Search{}, nil, DiffSummary{}, fmt.Errorf("name is required")
	}
	if query.EntityType == "" {
		query.EntityType = "Person"
	}
	org := creds.OrganizationId.String()
	raw, _ := json.Marshal(query)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	cooldown := s.Cooldown
	if cooldown == 0 {
		cooldown = time.Minute
	}
	if !query.Force {
		if previous, matches, ok, err := s.Repo.FindRecent(ctx, org, hash, s.now().Add(-cooldown)); err != nil {
			return Search{}, nil, DiffSummary{}, err
		} else if ok {
			previous.FromCache = true
			return previous, matches, DiffSummary{}, nil
		}
	}
	search := Search{
		ID: uuid.NewString(), OrgID: org, ClientRef: query.ClientRef, Query: query, QueryHash: hash,
		Source: "opensanctions", Status: "ok", Transmitted: Transmitted(query),
		CreatedBy: actorOf(creds), CreatedAt: s.now(),
	}
	matches, err := s.Sanctions.Match(ctx, query)
	if err != nil {
		search.Status = "error"
		search.Error = "screening provider unavailable"
		_ = s.Repo.InsertSearch(ctx, search, nil)
		_ = s.audit(ctx, org, "search", search.ID, actorOf(creds), "error", search.Error)
		return search, nil, DiffSummary{}, err
	}
	for i := range matches {
		matches[i].ID = uuid.NewString()
		matches[i].OrgID = org
		matches[i].SearchID = search.ID
		matches[i].ClientRef = query.ClientRef
		if matches[i].ConsultedAt.IsZero() {
			matches[i].ConsultedAt = search.CreatedAt
		}
		if matches[i].Qualification == "" {
			matches[i].Qualification = QualToReview
		}
	}
	previous, _ := s.Repo.MatchesByClient(ctx, org, query.ClientRef)
	diff := CompareMatches(latestByEntity(previous), matches)
	if err := s.Repo.InsertSearch(ctx, search, matches); err != nil {
		return Search{}, nil, DiffSummary{}, err
	}
	_ = s.audit(ctx, org, "search", search.ID, actorOf(creds), "ok", "")
	s.openScreeningCases(ctx, creds, query.ClientRef, diff)
	return search, matches, diff, nil
}

func latestByEntity(matches []Match) []Match {
	byID := map[string]Match{}
	for _, match := range matches {
		if current, ok := byID[match.EntityID]; !ok || match.ConsultedAt.After(current.ConsultedAt) {
			byID[match.EntityID] = match
		}
	}
	out := make([]Match, 0, len(byID))
	for _, match := range byID {
		out = append(out, match)
	}
	return out
}

func (s *Service) openScreeningCases(ctx context.Context, creds models.Credentials, client string, diff DiffSummary) {
	scenarios, err := s.ensureScenarios(ctx, creds.OrganizationId.String())
	if err != nil {
		return
	}
	var scenario Scenario
	for _, item := range scenarios {
		if item.Code == "UC11" {
			scenario = item
		}
	}
	for _, found := range ScreeningAlerts(scenario, diff) {
		alertKey := found.DedupKey + ":" + fmt.Sprint(found.Evidence["entity_id"]) + ":" + found.Evidence["change"].(string)
		_, _, _ = s.Repo.SaveCase(ctx, Case{
			ID: uuid.NewString(), OrgID: creds.OrganizationId.String(), AlertKey: alertKey, ClientRef: client,
			Status: CaseNew, Priority: found.Priority, ScenarioCode: found.Code,
			Alert: map[string]any{"explanation": found.Explanation, "evidence": found.Evidence},
			OpenedBy: "system", CreatedAt: s.now(), UpdatedAt: s.now(),
		})
	}
}

func (s *Service) Qualify(ctx context.Context, creds models.Credentials, id, status, reason string) (Match, error) {
	if err := s.require(creds, ActQualify); err != nil {
		return Match{}, err
	}
	switch status {
	case QualToReview, QualInAnalysis, QualDismissed, QualConfirmed, QualEscalate, QualError:
	default:
		return Match{}, fmt.Errorf("unknown qualification")
	}
	if (status == QualDismissed || status == QualConfirmed || status == QualEscalate) && reason == "" {
		return Match{}, fmt.Errorf("a reason is required")
	}
	match, err := s.Repo.Qualify(ctx, creds.OrganizationId.String(), id, status, reason, actorOf(creds))
	if err != nil {
		return Match{}, err
	}
	_ = s.audit(ctx, creds.OrganizationId.String(), "match", id, actorOf(creds), status, reason)
	return match, nil
}

func (s *Service) Transition(ctx context.Context, creds models.Credentials, id, to, reason string) (Case, error) {
	action := ActValidate
	if to == CaseClosed {
		action = ActClose
	}
	if to == CaseInvestigating && reason != "" {
		action = ActValidate
	}
	if err := s.require(creds, action); err != nil {
		return Case{}, err
	}
	if to == CaseClosed {
		if err := s.require(creds, ActClose); err != nil {
			return Case{}, err
		}
	}
	item, err := s.Repo.GetCase(ctx, creds.OrganizationId.String(), id)
	if err != nil {
		return Case{}, err
	}
	if to == CaseInvestigating && item.Status == CaseClosed {
		if err := s.require(creds, ActClose); err != nil {
			return Case{}, err
		}
	}
	next, err := ApplyTransition(TransitionInput{To: to, Actor: actorOf(creds), Reason: reason, Case: item})
	if err != nil {
		return Case{}, err
	}
	next.UpdatedAt = s.now()
	if err := s.Repo.UpdateCase(ctx, next); err != nil {
		return Case{}, err
	}
	_ = s.audit(ctx, item.OrgID, "case", id, actorOf(creds), to, reason)
	return next, nil
}

func (s *Service) Assign(ctx context.Context, creds models.Credentials, id, assignee, team string) (Case, error) {
	if err := s.require(creds, ActAssign); err != nil {
		return Case{}, err
	}
	item, err := s.Repo.GetCase(ctx, creds.OrganizationId.String(), id)
	if err != nil {
		return Case{}, err
	}
	item.Assignee = assignee
	item.Team = team
	item.UpdatedAt = s.now()
	if err := s.Repo.UpdateCase(ctx, item); err != nil {
		return Case{}, err
	}
	_ = s.audit(ctx, item.OrgID, "case", id, actorOf(creds), "assign", assignee)
	return item, nil
}

func (s *Service) OpenAlertCase(ctx context.Context, creds models.Credentials, found Alert, client ClientInput) (Case, bool, error) {
	if err := s.require(creds, ActValidate); err != nil {
		return Case{}, false, err
	}
	score := CalculateScore(ScoreInput{Client: client, Reason: found.Code, Formula: DefaultFormula()})
	item := Case{
		ID: uuid.NewString(), OrgID: creds.OrganizationId.String(), AlertKey: found.DedupKey,
		ClientRef: found.ClientID, Status: CaseNew, Priority: found.Priority, ScenarioCode: found.Code,
		Alert: map[string]any{"explanation": found.Explanation, "evidence": found.Evidence, "version": found.Version},
		Score: map[string]any{"total": score.Total, "sanctions_status": score.SanctionsStatus},
		OpenedBy: actorOf(creds), CreatedAt: s.now(), UpdatedAt: s.now(),
	}
	saved, created, err := s.Repo.SaveCase(ctx, item)
	if err == nil && created {
		_ = s.audit(ctx, item.OrgID, "case", saved.ID, actorOf(creds), "open", found.Code)
	}
	return saved, created, err
}

func (s *Service) Calculate(ctx context.Context, creds models.Credentials, client ClientInput, matches []Match, reason string) (Score, error) {
	if err := s.require(creds, ActScore); err != nil && !Allowed(creds, ActRead) {
		return Score{}, err
	}
	if !Allowed(creds, ActScore) && !Allowed(creds, ActValidate) {
		return Score{}, errDenied
	}
	formula, err := s.Repo.ActiveFormula(ctx, creds.OrganizationId.String())
	if err != nil {
		return Score{}, err
	}
	countries := map[string]bool{}
	if setting, _ := s.activeSetting(ctx, creds.OrganizationId.String(), "high_risk_countries"); setting.Value != nil {
		if raw, ok := setting.Value["countries"].([]any); ok {
			for _, item := range raw {
				if text, ok := item.(string); ok {
					countries[text] = true
				}
			}
		}
	}
	score := CalculateScore(ScoreInput{Client: client, HighRiskCountry: countries, Matches: matches, Reason: reason, Formula: formula})
	score.ID = uuid.NewString()
	score.OrgID = creds.OrganizationId.String()
	score.ReferenceAt = s.now()
	score.CreatedBy = actorOf(creds)
	score.CreatedAt = s.now()
	if err := s.Repo.SaveScore(ctx, score); err != nil {
		return Score{}, err
	}
	_ = s.audit(ctx, score.OrgID, "score", score.ID, actorOf(creds), "calculate", reason)
	return score, nil
}

func (s *Service) SaveFormula(ctx context.Context, creds models.Credentials, formula Formula) (Formula, error) {
	if err := s.require(creds, ActScore); err != nil {
		return Formula{}, err
	}
	if err := ValidateFormula(formula); err != nil {
		return Formula{}, err
	}
	current, _ := s.Repo.ListFormulas(ctx, creds.OrganizationId.String())
	formula.ID = uuid.NewString()
	formula.OrgID = creds.OrganizationId.String()
	formula.Status = "pending_approval"
	formula.Version = len(current) + 1
	if formula.Version < 2 {
		formula.Version = 2
	}
	formula.Author = actorOf(creds)
	formula.CreatedAt = s.now()
	formula.Description = "Proposed change. The active formula stays in use until approval."
	if err := s.Repo.SaveFormula(ctx, formula); err != nil {
		return Formula{}, err
	}
	return formula, nil
}

func (s *Service) ApproveFormula(ctx context.Context, creds models.Credentials, id string) (Formula, error) {
	if err := s.require(creds, ActApprove); err != nil {
		return Formula{}, err
	}
	formulas, err := s.Repo.ListFormulas(ctx, creds.OrganizationId.String())
	if err != nil {
		return Formula{}, err
	}
	for _, formula := range formulas {
		if formula.ID != id {
			continue
		}
		if formula.Author == actorOf(creds) {
			return Formula{}, fmt.Errorf("the author cannot approve this version")
		}
		formula.Status = "active"
		formula.Approver = actorOf(creds)
		if err := s.Repo.SaveFormula(ctx, formula); err != nil {
			return Formula{}, err
		}
		return formula, nil
	}
	return Formula{}, errNotFound
}

func (s *Service) Evaluate(ctx context.Context, creds models.Credentials, pop Population, commit bool) ([]Alert, error) {
	if err := s.require(creds, ActScenario); err != nil && !Allowed(creds, ActRead) {
		return nil, errDenied
	}
	scenarios, err := s.ensureScenarios(ctx, creds.OrganizationId.String())
	if err != nil {
		return nil, err
	}
	alerts := EvaluateScenarios(scenarios, pop)
	if commit {
		for _, found := range alerts {
			var client ClientInput
			for _, item := range pop.Clients {
				if item.ClientRef == found.ClientID {
					client = item
				}
			}
			_, _, _ = s.OpenAlertCase(ctx, creds, found, client)
		}
	}
	return alerts, nil
}

func (s *Service) ensureScenarios(ctx context.Context, org string) ([]Scenario, error) {
	current, err := s.Repo.ListScenarios(ctx, org)
	if err != nil {
		return nil, err
	}
	if len(current) > 0 {
		return current, nil
	}
	for _, scenario := range DefaultScenarios() {
		scenario.OrgID = org
		scenario.Author = "system"
		scenario.UpdatedAt = s.now()
		if err := s.Repo.SaveScenario(ctx, scenario); err != nil {
			return nil, err
		}
		current = append(current, scenario)
	}
	return current, nil
}

func (s *Service) audit(ctx context.Context, org, kind, id, actor, action, reason string) error {
	return s.Repo.AddEvent(ctx, Event{
		ID: uuid.NewString(), OrgID: org, ObjectType: kind, ObjectID: id,
		Action: action, Actor: actor, Reason: reason, CreatedAt: s.now(),
	})
}

func (s *Service) activeSetting(ctx context.Context, org, key string) (Setting, bool) {
	settings, err := s.Repo.ListSettings(ctx, org)
	if err != nil {
		return Setting{}, false
	}
	var found Setting
	ok := false
	for _, setting := range settings {
		if setting.Key == key && setting.Status == "active" && (!ok || setting.Version > found.Version) {
			found = setting
			ok = true
		}
	}
	return found, ok
}
