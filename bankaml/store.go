package bankaml

import (
	"context"
	"sync"
	"time"
)

type Repository interface {
	InsertSearch(context.Context, Search, []Match) error
	FindRecent(context.Context, string, string, time.Time) (Search, []Match, bool, error)
	ListSearches(context.Context, string, int) ([]Search, error)
	GetSearch(context.Context, string, string) (Search, []Match, error)
	MatchesByClient(context.Context, string, string) ([]Match, error)
	GetMatch(context.Context, string, string) (Match, error)
	Qualify(context.Context, string, string, string, string, string) (Match, error)
	AddEvent(context.Context, Event) error
	ListEvents(context.Context, string, string, string) ([]Event, error)
	SaveCase(context.Context, Case) (Case, bool, error)
	GetCase(context.Context, string, string) (Case, error)
	ListCases(context.Context, string) ([]Case, error)
	UpdateCase(context.Context, Case) error
	RunningBatch(context.Context, string) (Batch, bool, error)
	SaveBatch(context.Context, Batch) error
	GetBatch(context.Context, string, string) (Batch, error)
	ListBatches(context.Context, string) ([]Batch, error)
	AddBatchItem(context.Context, string, BatchItem) error
	SaveScore(context.Context, Score) error
	ListScores(context.Context, string, string) ([]Score, error)
	ActiveFormula(context.Context, string) (Formula, error)
	SaveFormula(context.Context, Formula) error
	ListFormulas(context.Context, string) ([]Formula, error)
	ListScenarios(context.Context, string) ([]Scenario, error)
	SaveScenario(context.Context, Scenario) error
	ListSettings(context.Context, string) ([]Setting, error)
	SaveSetting(context.Context, Setting) error
}

type Memory struct {
	mu         sync.Mutex
	searches   []Search
	matches    []Match
	events     []Event
	cases      []Case
	batches    []Batch
	items      []BatchItem
	scores     []Score
	formulas   []Formula
	scenarios  []Scenario
	settings   []Setting
}

func NewMemory() *Memory { return &Memory{} }

func (m *Memory) InsertSearch(_ context.Context, search Search, matches []Match) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.searches = append(m.searches, search)
	m.matches = append(m.matches, matches...)
	return nil
}

func (m *Memory) FindRecent(_ context.Context, org, hash string, since time.Time) (Search, []Match, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.searches) - 1; i >= 0; i-- {
		search := m.searches[i]
		if search.OrgID == org && search.QueryHash == hash && search.Status == "ok" && !search.CreatedAt.Before(since) {
			return search, m.matchesOf(search.ID), true, nil
		}
	}
	return Search{}, nil, false, nil
}

func (m *Memory) ListSearches(_ context.Context, org string, limit int) ([]Search, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Search{}
	for i := len(m.searches) - 1; i >= 0 && len(out) < limit; i-- {
		if m.searches[i].OrgID == org {
			out = append(out, m.searches[i])
		}
	}
	return out, nil
}

func (m *Memory) GetSearch(_ context.Context, org, id string) (Search, []Match, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, search := range m.searches {
		if search.OrgID == org && search.ID == id {
			return search, m.matchesOf(id), nil
		}
	}
	return Search{}, nil, errNotFound
}

func (m *Memory) MatchesByClient(_ context.Context, org, client string) ([]Match, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Match{}
	if client == "" {
		return out, nil
	}
	for _, match := range m.matches {
		if match.OrgID == org && match.ClientRef == client {
			out = append(out, match)
		}
	}
	return out, nil
}

func (m *Memory) GetMatch(_ context.Context, org, id string) (Match, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, match := range m.matches {
		if match.OrgID == org && match.ID == id {
			return match, nil
		}
	}
	return Match{}, errNotFound
}

func (m *Memory) Qualify(_ context.Context, org, id, status, reason, actor string) (Match, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for i := range m.matches {
		if m.matches[i].OrgID == org && m.matches[i].ID == id {
			m.matches[i].Qualification = status
			m.matches[i].QualificationReason = reason
			m.matches[i].QualifiedBy = actor
			m.matches[i].QualifiedAt = &now
			return m.matches[i], nil
		}
	}
	return Match{}, errNotFound
}

func (m *Memory) AddEvent(_ context.Context, event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *Memory) ListEvents(_ context.Context, org, kind, id string) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Event{}
	for _, event := range m.events {
		if event.OrgID == org && (kind == "" || event.ObjectType == kind) && (id == "" || event.ObjectID == id) {
			out = append(out, event)
		}
	}
	return out, nil
}

func (m *Memory) SaveCase(_ context.Context, item Case) (Case, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.cases {
		if m.cases[i].OrgID == item.OrgID && m.cases[i].AlertKey == item.AlertKey {
			return m.cases[i], false, nil
		}
	}
	m.cases = append(m.cases, item)
	return item, true, nil
}

func (m *Memory) GetCase(_ context.Context, org, id string) (Case, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.cases {
		if item.OrgID == org && item.ID == id {
			return item, nil
		}
	}
	return Case{}, errNotFound
}

func (m *Memory) ListCases(_ context.Context, org string) ([]Case, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Case{}
	for _, item := range m.cases {
		if item.OrgID == org {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *Memory) UpdateCase(_ context.Context, item Case) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.cases {
		if m.cases[i].OrgID == item.OrgID && m.cases[i].ID == item.ID {
			m.cases[i] = item
			return nil
		}
	}
	return errNotFound
}

func (m *Memory) RunningBatch(_ context.Context, org string) (Batch, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, batch := range m.batches {
		if batch.OrgID == org && (batch.Status == BatchPending || batch.Status == BatchRunning) {
			return batch, true, nil
		}
	}
	return Batch{}, false, nil
}

func (m *Memory) SaveBatch(_ context.Context, batch Batch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.batches {
		if m.batches[i].ID == batch.ID {
			items := m.batches[i].Items
			batch.Items = items
			m.batches[i] = batch
			return nil
		}
	}
	m.batches = append(m.batches, batch)
	return nil
}

func (m *Memory) GetBatch(_ context.Context, org, id string) (Batch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, batch := range m.batches {
		if batch.OrgID == org && batch.ID == id {
			for _, item := range m.items {
				if item.BatchID == id {
					batch.Items = append(batch.Items, item)
				}
			}
			return batch, nil
		}
	}
	return Batch{}, errNotFound
}

func (m *Memory) ListBatches(_ context.Context, org string) ([]Batch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Batch{}
	for _, batch := range m.batches {
		if batch.OrgID == org {
			batch.Population = nil
			out = append(out, batch)
		}
	}
	return out, nil
}

func (m *Memory) AddBatchItem(_ context.Context, _ string, item BatchItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, item)
	return nil
}

func (m *Memory) SaveScore(_ context.Context, score Score) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scores = append(m.scores, score)
	return nil
}

func (m *Memory) ListScores(_ context.Context, org, client string) ([]Score, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Score{}
	for _, score := range m.scores {
		if score.OrgID == org && (client == "" || score.ClientRef == client) {
			out = append(out, score)
		}
	}
	return out, nil
}

func (m *Memory) ActiveFormula(_ context.Context, org string) (Formula, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var found Formula
	ok := false
	for _, formula := range m.formulas {
		if formula.OrgID == org && formula.Status == "active" && (!ok || formula.Version > found.Version) {
			found = formula
			ok = true
		}
	}
	if !ok {
		formula := DefaultFormula()
		formula.OrgID = org
		return formula, nil
	}
	return found, nil
}

func (m *Memory) SaveFormula(_ context.Context, formula Formula) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.formulas {
		if m.formulas[i].ID == formula.ID {
			m.formulas[i] = formula
			return nil
		}
	}
	m.formulas = append(m.formulas, formula)
	return nil
}

func (m *Memory) ListFormulas(_ context.Context, org string) ([]Formula, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Formula{}
	for _, formula := range m.formulas {
		if formula.OrgID == org {
			out = append(out, formula)
		}
	}
	return out, nil
}

func (m *Memory) ListScenarios(_ context.Context, org string) ([]Scenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	latest := map[string]Scenario{}
	for _, scenario := range m.scenarios {
		if scenario.OrgID != org {
			continue
		}
		if current, ok := latest[scenario.Code]; !ok || scenario.Version >= current.Version {
			latest[scenario.Code] = scenario
		}
	}
	out := []Scenario{}
	for _, scenario := range latest {
		out = append(out, scenario)
	}
	return out, nil
}

func (m *Memory) SaveScenario(_ context.Context, scenario Scenario) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.scenarios {
		if m.scenarios[i].OrgID == scenario.OrgID && m.scenarios[i].Code == scenario.Code && m.scenarios[i].Version == scenario.Version {
			m.scenarios[i] = scenario
			return nil
		}
	}
	m.scenarios = append(m.scenarios, scenario)
	return nil
}

func (m *Memory) ListSettings(_ context.Context, org string) ([]Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Setting{}
	for _, setting := range m.settings {
		if setting.OrgID == org {
			out = append(out, setting)
		}
	}
	return out, nil
}

func (m *Memory) SaveSetting(_ context.Context, setting Setting) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.settings {
		if m.settings[i].ID == setting.ID {
			m.settings[i] = setting
			return nil
		}
	}
	m.settings = append(m.settings, setting)
	return nil
}

func (m *Memory) matchesOf(searchID string) []Match {
	out := []Match{}
	for _, match := range m.matches {
		if match.SearchID == searchID {
			out = append(out, match)
		}
	}
	return out
}
