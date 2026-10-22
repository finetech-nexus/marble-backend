package bankaml

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PG struct{ pool *pgxpool.Pool }

func NewPG(ctx context.Context) (*PG, error) {
	dsn := os.Getenv("PG_CONNECTION_STRING")
	if dsn == "" {
		host := os.Getenv("PG_HOSTNAME")
		if host == "" {
			return nil, fmt.Errorf("postgres is not configured")
		}
		database := os.Getenv("PG_DATABASE")
		if database == "" {
			database = "marble"
		}
		port := os.Getenv("PG_PORT")
		if port == "" {
			port = "5432"
		}
		ssl := os.Getenv("PG_SSL_MODE")
		if ssl == "" {
			ssl = "prefer"
		}
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			url.QueryEscape(os.Getenv("PG_USER")), url.QueryEscape(os.Getenv("PG_PASSWORD")), host, port, database, ssl)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &PG{pool: pool}, nil
}

func js(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil || raw == nil {
		return []byte("{}")
	}
	return raw
}

func (p *PG) InsertSearch(ctx context.Context, search Search, matches []Match) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `insert into bankaml_searches
		(id, org_id, client_ref, query, query_hash, source, status, error, transmitted, created_by, created_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		search.ID, search.OrgID, search.ClientRef, js(search.Query), search.QueryHash, search.Source, search.Status, nullText(search.Error), js(search.Transmitted), search.CreatedBy, search.CreatedAt)
	if err != nil {
		return err
	}
	for _, match := range matches {
		_, err = tx.Exec(ctx, `insert into bankaml_matches
			(id, org_id, search_id, client_ref, entity_id, entity_name, entity_type, datasets, matched, mismatched, score, explanation, consulted_at, qualification, raw)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'{}')`,
			match.ID, match.OrgID, match.SearchID, nullText(match.ClientRef), match.EntityID, match.EntityName, match.EntityType, js(match.Datasets), js(match.Matched), js(match.Mismatched), match.Score, js(match.Explanation), match.ConsultedAt, match.Qualification)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *PG) FindRecent(ctx context.Context, org, hash string, since time.Time) (Search, []Match, bool, error) {
	row := p.pool.QueryRow(ctx, `select id from bankaml_searches where org_id=$1 and query_hash=$2 and status='ok' and created_at>=$3 order by created_at desc limit 1`, org, hash, since)
	var id string
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Search{}, nil, false, nil
		}
		return Search{}, nil, false, err
	}
	search, matches, err := p.GetSearch(ctx, org, id)
	return search, matches, err == nil, err
}

func (p *PG) ListSearches(ctx context.Context, org string, limit int) ([]Search, error) {
	rows, err := p.pool.Query(ctx, `select id, org_id, coalesce(client_ref,''), query, query_hash, source, status, coalesce(error,''), transmitted, created_by, created_at
		from bankaml_searches where org_id=$1 order by created_at desc limit $2`, org, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Search{}
	for rows.Next() {
		item, err := scanSearch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (p *PG) GetSearch(ctx context.Context, org, id string) (Search, []Match, error) {
	row := p.pool.QueryRow(ctx, `select id, org_id, coalesce(client_ref,''), query, query_hash, source, status, coalesce(error,''), transmitted, created_by, created_at
		from bankaml_searches where org_id=$1 and id=$2`, org, id)
	search, err := scanSearch(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Search{}, nil, errNotFound
		}
		return Search{}, nil, err
	}
	matches, err := p.queryMatches(ctx, `select `+matchCols+` from bankaml_matches where org_id=$1 and search_id=$2`, org, id)
	return search, matches, err
}

func (p *PG) MatchesByClient(ctx context.Context, org, client string) ([]Match, error) {
	if client == "" {
		return []Match{}, nil
	}
	return p.queryMatches(ctx, `select `+matchCols+` from bankaml_matches where org_id=$1 and client_ref=$2`, org, client)
}

func (p *PG) GetMatch(ctx context.Context, org, id string) (Match, error) {
	items, err := p.queryMatches(ctx, `select `+matchCols+` from bankaml_matches where org_id=$1 and id=$2`, org, id)
	if err != nil {
		return Match{}, err
	}
	if len(items) == 0 {
		return Match{}, errNotFound
	}
	return items[0], nil
}

func (p *PG) Qualify(ctx context.Context, org, id, status, reason, actor string) (Match, error) {
	tag, err := p.pool.Exec(ctx, `update bankaml_matches set qualification=$3, qualification_reason=$4, qualified_by=$5, qualified_at=now()
		where org_id=$1 and id=$2`, org, id, status, reason, actor)
	if err != nil {
		return Match{}, err
	}
	if tag.RowsAffected() == 0 {
		return Match{}, errNotFound
	}
	return p.GetMatch(ctx, org, id)
}

func (p *PG) AddEvent(ctx context.Context, event Event) error {
	_, err := p.pool.Exec(ctx, `insert into bankaml_events (id, org_id, object_type, object_id, action, actor, reason, payload, created_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, event.ID, event.OrgID, event.ObjectType, event.ObjectID, event.Action, event.Actor, nullText(event.Reason), js(event.Payload), event.CreatedAt)
	return err
}

func (p *PG) ListEvents(ctx context.Context, org, kind, id string) ([]Event, error) {
	rows, err := p.pool.Query(ctx, `select id, org_id, object_type, object_id, action, actor, coalesce(reason,''), payload, created_at
		from bankaml_events where org_id=$1 and ($2='' or object_type=$2) and ($3='' or object_id=$3) order by created_at`, org, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var event Event
		var payload []byte
		if err := rows.Scan(&event.ID, &event.OrgID, &event.ObjectType, &event.ObjectID, &event.Action, &event.Actor, &event.Reason, &payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &event.Payload)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (p *PG) SaveCase(ctx context.Context, item Case) (Case, bool, error) {
	var existing string
	err := p.pool.QueryRow(ctx, `select id from bankaml_cases where org_id=$1 and alert_key=$2`, item.OrgID, item.AlertKey).Scan(&existing)
	if err == nil {
		saved, getErr := p.GetCase(ctx, item.OrgID, existing)
		return saved, false, getErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Case{}, false, err
	}
	if err := p.UpdateCase(ctx, item); err != nil && !errors.Is(err, errNotFound) {
		return Case{}, false, err
	}
	_, err = p.pool.Exec(ctx, `insert into bankaml_cases
		(id, org_id, alert_key, client_ref, status, priority, assignee, team, due_at, scenario_code, alert, score, screening, closure_reason, investigator, level1_actor, level2_actor, opened_by, created_at, updated_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		item.ID, item.OrgID, item.AlertKey, item.ClientRef, item.Status, item.Priority, nullText(item.Assignee), nullText(item.Team), item.DueAt, nullText(item.ScenarioCode), js(item.Alert), js(item.Score), js(item.Screening), nullText(item.ClosureReason), nullText(item.Investigator), nullText(item.Level1Actor), nullText(item.Level2Actor), item.OpenedBy, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return Case{}, false, err
	}
	return item, true, nil
}

func (p *PG) GetCase(ctx context.Context, org, id string) (Case, error) {
	items, err := p.queryCases(ctx, `where org_id=$1 and id=$2`, org, id)
	if err != nil {
		return Case{}, err
	}
	if len(items) == 0 {
		return Case{}, errNotFound
	}
	return items[0], nil
}

func (p *PG) ListCases(ctx context.Context, org string) ([]Case, error) {
	return p.queryCases(ctx, `where org_id=$1 order by updated_at desc`, org)
}

func (p *PG) UpdateCase(ctx context.Context, item Case) error {
	tag, err := p.pool.Exec(ctx, `update bankaml_cases set status=$3, priority=$4, assignee=$5, team=$6, due_at=$7, alert=$8, score=$9, screening=$10,
		closure_reason=$11, investigator=$12, level1_actor=$13, level2_actor=$14, updated_at=$15 where org_id=$1 and id=$2`,
		item.OrgID, item.ID, item.Status, item.Priority, nullText(item.Assignee), nullText(item.Team), item.DueAt, js(item.Alert), js(item.Score), js(item.Screening), nullText(item.ClosureReason), nullText(item.Investigator), nullText(item.Level1Actor), nullText(item.Level2Actor), item.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return nil
}

func (p *PG) RunningBatch(ctx context.Context, org string) (Batch, bool, error) {
	items, err := p.queryBatches(ctx, `where org_id=$1 and status in ('pending','running') limit 1`, org)
	if err != nil || len(items) == 0 {
		return Batch{}, false, err
	}
	return items[0], true, nil
}

func (p *PG) SaveBatch(ctx context.Context, batch Batch) error {
	tag, err := p.pool.Exec(ctx, `update bankaml_batches set status=$3, cursor_pos=$4, succeeded=$5, failed=$6, new_matches=$7, changed_matches=$8, missing_matches=$9, remaining=$10, error=$11, started_at=$12, finished_at=$13
		where id=$1 and org_id=$2`, batch.ID, batch.OrgID, batch.Status, batch.Cursor, batch.Succeeded, batch.Failed, batch.NewMatches, batch.ChangedMatches, batch.MissingMatches, batch.Remaining, nullText(batch.Error), batch.StartedAt, batch.FinishedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = p.pool.Exec(ctx, `insert into bankaml_batches
		(id, org_id, mode, status, population, chunk_size, cursor_pos, total_count, succeeded, failed, new_matches, changed_matches, missing_matches, remaining, triggered_by, error, started_at, finished_at, created_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		batch.ID, batch.OrgID, batch.Mode, batch.Status, js(batch.Population), batch.ChunkSize, batch.Cursor, batch.Total, batch.Succeeded, batch.Failed, batch.NewMatches, batch.ChangedMatches, batch.MissingMatches, batch.Remaining, batch.TriggeredBy, nullText(batch.Error), batch.StartedAt, batch.FinishedAt, batch.CreatedAt)
	return err
}

func (p *PG) GetBatch(ctx context.Context, org, id string) (Batch, error) {
	items, err := p.queryBatches(ctx, `where org_id=$1 and id=$2`, org, id)
	if err != nil {
		return Batch{}, err
	}
	if len(items) == 0 {
		return Batch{}, errNotFound
	}
	batch := items[0]
	rows, err := p.pool.Query(ctx, `select id, batch_id, client_ref, status, coalesce(error,''), new_matches, changed_matches, missing_matches from bankaml_batch_items where batch_id=$1`, id)
	if err != nil {
		return batch, err
	}
	defer rows.Close()
	for rows.Next() {
		var item BatchItem
		if err := rows.Scan(&item.ID, &item.BatchID, &item.ClientRef, &item.Status, &item.Error, &item.NewMatches, &item.ChangedMatches, &item.MissingMatches); err != nil {
			return batch, err
		}
		batch.Items = append(batch.Items, item)
	}
	return batch, rows.Err()
}

func (p *PG) ListBatches(ctx context.Context, org string) ([]Batch, error) {
	items, err := p.queryBatches(ctx, `where org_id=$1 order by created_at desc`, org)
	for i := range items {
		items[i].Population = nil
	}
	return items, err
}

func (p *PG) AddBatchItem(ctx context.Context, org string, item BatchItem) error {
	_, err := p.pool.Exec(ctx, `insert into bankaml_batch_items (id, batch_id, org_id, client_ref, status, error, new_matches, changed_matches, missing_matches)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, item.ID, item.BatchID, org, item.ClientRef, item.Status, nullText(item.Error), item.NewMatches, item.ChangedMatches, item.MissingMatches)
	return err
}

func (p *PG) SaveScore(ctx context.Context, score Score) error {
	_, err := p.pool.Exec(ctx, `insert into bankaml_scores
		(id, org_id, client_ref, total, components, factors, formula_version, sanctions_status, review_required, missing, reference_at, reason, created_by, created_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		score.ID, score.OrgID, score.ClientRef, score.Total, js(score.Components), js(score.Factors), score.FormulaVersion, score.SanctionsStatus, score.ReviewRequired, js(score.Missing), score.ReferenceAt, score.Reason, score.CreatedBy, score.CreatedAt)
	return err
}

func (p *PG) ListScores(ctx context.Context, org, client string) ([]Score, error) {
	rows, err := p.pool.Query(ctx, `select id, org_id, client_ref, total, components, factors, formula_version, sanctions_status, review_required, missing, reference_at, reason, created_by, created_at
		from bankaml_scores where org_id=$1 and ($2='' or client_ref=$2) order by created_at desc`, org, client)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Score{}
	for rows.Next() {
		var score Score
		var components, factors, missing []byte
		if err := rows.Scan(&score.ID, &score.OrgID, &score.ClientRef, &score.Total, &components, &factors, &score.FormulaVersion, &score.SanctionsStatus, &score.ReviewRequired, &missing, &score.ReferenceAt, &score.Reason, &score.CreatedBy, &score.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(components, &score.Components)
		_ = json.Unmarshal(factors, &score.Factors)
		_ = json.Unmarshal(missing, &score.Missing)
		out = append(out, score)
	}
	return out, rows.Err()
}

func (p *PG) ActiveFormula(ctx context.Context, org string) (Formula, error) {
	items, err := p.ListFormulas(ctx, org)
	if err != nil {
		return Formula{}, err
	}
	var found Formula
	ok := false
	for _, formula := range items {
		if formula.Status == "active" && (!ok || formula.Version > found.Version) {
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

func (p *PG) SaveFormula(ctx context.Context, formula Formula) error {
	tag, err := p.pool.Exec(ctx, `update bankaml_formulas set status=$2, approver=$3 where id=$1`, formula.ID, formula.Status, nullText(formula.Approver))
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = p.pool.Exec(ctx, `insert into bankaml_formulas (id, org_id, weights, classes, factors, description, status, version, author, approver, created_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, formula.ID, formula.OrgID, js(formula.Weights), js(formula.Classes), js(formula.Factors), formula.Description, formula.Status, formula.Version, formula.Author, nullText(formula.Approver), formula.CreatedAt)
	return err
}

func (p *PG) ListFormulas(ctx context.Context, org string) ([]Formula, error) {
	rows, err := p.pool.Query(ctx, `select id, org_id, weights, classes, factors, description, status, version, author, coalesce(approver,''), created_at from bankaml_formulas where org_id=$1 order by version`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Formula{}
	for rows.Next() {
		var formula Formula
		var weights, classes, factors []byte
		if err := rows.Scan(&formula.ID, &formula.OrgID, &weights, &classes, &factors, &formula.Description, &formula.Status, &formula.Version, &formula.Author, &formula.Approver, &formula.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(weights, &formula.Weights)
		_ = json.Unmarshal(classes, &formula.Classes)
		_ = json.Unmarshal(factors, &formula.Factors)
		out = append(out, formula)
	}
	return out, rows.Err()
}

func (p *PG) ListScenarios(ctx context.Context, org string) ([]Scenario, error) {
	rows, err := p.pool.Query(ctx, `select distinct on (code) org_id, code, description, params, severity, priority, version, status, enabled, author, coalesce(approver,''), updated_at
		from bankaml_scenarios where org_id=$1 order by code, version desc`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScenarios(rows)
}

func (p *PG) SaveScenario(ctx context.Context, scenario Scenario) error {
	tag, err := p.pool.Exec(ctx, `update bankaml_scenarios set description=$4, params=$5, severity=$6, priority=$7, status=$8, enabled=$9, approver=$10, updated_at=$11
		where org_id=$1 and code=$2 and version=$3`, scenario.OrgID, scenario.Code, scenario.Version, scenario.Description, js(scenario.Params), scenario.Severity, scenario.Priority, scenario.Status, scenario.Enabled, nullText(scenario.Approver), scenario.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = p.pool.Exec(ctx, `insert into bankaml_scenarios (org_id, code, description, params, severity, priority, version, status, enabled, author, approver, updated_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, scenario.OrgID, scenario.Code, scenario.Description, js(scenario.Params), scenario.Severity, scenario.Priority, scenario.Version, scenario.Status, scenario.Enabled, scenario.Author, nullText(scenario.Approver), scenario.UpdatedAt)
	return err
}

func (p *PG) ListSettings(ctx context.Context, org string) ([]Setting, error) {
	rows, err := p.pool.Query(ctx, `select id, org_id, key, value, description, status, version, effective_at, author, coalesce(approver,''), created_at from bankaml_settings where org_id=$1 order by key, version`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Setting{}
	for rows.Next() {
		var setting Setting
		var value []byte
		if err := rows.Scan(&setting.ID, &setting.OrgID, &setting.Key, &value, &setting.Description, &setting.Status, &setting.Version, &setting.EffectiveAt, &setting.Author, &setting.Approver, &setting.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(value, &setting.Value)
		out = append(out, setting)
	}
	return out, rows.Err()
}

func (p *PG) SaveSetting(ctx context.Context, setting Setting) error {
	tag, err := p.pool.Exec(ctx, `update bankaml_settings set status=$2, approver=$3, effective_at=$4 where id=$1`, setting.ID, setting.Status, nullText(setting.Approver), setting.EffectiveAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = p.pool.Exec(ctx, `insert into bankaml_settings (id, org_id, key, value, description, status, version, effective_at, author, approver, created_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, setting.ID, setting.OrgID, setting.Key, js(setting.Value), setting.Description, setting.Status, setting.Version, setting.EffectiveAt, setting.Author, nullText(setting.Approver), setting.CreatedAt)
	return err
}

const matchCols = `id, org_id, search_id, coalesce(client_ref,''), entity_id, entity_name, entity_type, datasets, matched, mismatched, score, explanation, consulted_at, qualification, coalesce(qualification_reason,''), coalesce(qualified_by,''), qualified_at`

func (p *PG) queryMatches(ctx context.Context, sql string, args ...any) ([]Match, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Match{}
	for rows.Next() {
		var match Match
		var datasets, matched, mismatched, explanation []byte
		if err := rows.Scan(&match.ID, &match.OrgID, &match.SearchID, &match.ClientRef, &match.EntityID, &match.EntityName, &match.EntityType, &datasets, &matched, &mismatched, &match.Score, &explanation, &match.ConsultedAt, &match.Qualification, &match.QualificationReason, &match.QualifiedBy, &match.QualifiedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(datasets, &match.Datasets)
		_ = json.Unmarshal(matched, &match.Matched)
		_ = json.Unmarshal(mismatched, &match.Mismatched)
		_ = json.Unmarshal(explanation, &match.Explanation)
		out = append(out, match)
	}
	return out, rows.Err()
}

func (p *PG) queryCases(ctx context.Context, where string, args ...any) ([]Case, error) {
	rows, err := p.pool.Query(ctx, `select id, org_id, alert_key, client_ref, status, priority, coalesce(assignee,''), coalesce(team,''), due_at, coalesce(scenario_code,''), alert, score, screening, coalesce(closure_reason,''), coalesce(investigator,''), coalesce(level1_actor,''), coalesce(level2_actor,''), opened_by, created_at, updated_at from bankaml_cases `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Case{}
	for rows.Next() {
		var item Case
		var alert, score, screening []byte
		if err := rows.Scan(&item.ID, &item.OrgID, &item.AlertKey, &item.ClientRef, &item.Status, &item.Priority, &item.Assignee, &item.Team, &item.DueAt, &item.ScenarioCode, &alert, &score, &screening, &item.ClosureReason, &item.Investigator, &item.Level1Actor, &item.Level2Actor, &item.OpenedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(alert, &item.Alert)
		_ = json.Unmarshal(score, &item.Score)
		_ = json.Unmarshal(screening, &item.Screening)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (p *PG) queryBatches(ctx context.Context, where string, args ...any) ([]Batch, error) {
	rows, err := p.pool.Query(ctx, `select id, org_id, mode, status, population, chunk_size, cursor_pos, total_count, succeeded, failed, new_matches, changed_matches, missing_matches, remaining, triggered_by, coalesce(error,''), started_at, finished_at, created_at from bankaml_batches `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Batch{}
	for rows.Next() {
		var batch Batch
		var population []byte
		if err := rows.Scan(&batch.ID, &batch.OrgID, &batch.Mode, &batch.Status, &population, &batch.ChunkSize, &batch.Cursor, &batch.Total, &batch.Succeeded, &batch.Failed, &batch.NewMatches, &batch.ChangedMatches, &batch.MissingMatches, &batch.Remaining, &batch.TriggeredBy, &batch.Error, &batch.StartedAt, &batch.FinishedAt, &batch.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(population, &batch.Population)
		out = append(out, batch)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanSearch(row scanner) (Search, error) {
	var search Search
	var query, transmitted []byte
	err := row.Scan(&search.ID, &search.OrgID, &search.ClientRef, &query, &search.QueryHash, &search.Source, &search.Status, &search.Error, &transmitted, &search.CreatedBy, &search.CreatedAt)
	if err != nil {
		return Search{}, err
	}
	_ = json.Unmarshal(query, &search.Query)
	_ = json.Unmarshal(transmitted, &search.Transmitted)
	return search, nil
}

func scanScenarios(rows pgx.Rows) ([]Scenario, error) {
	out := []Scenario{}
	for rows.Next() {
		var scenario Scenario
		var params []byte
		if err := rows.Scan(&scenario.OrgID, &scenario.Code, &scenario.Description, &params, &scenario.Severity, &scenario.Priority, &scenario.Version, &scenario.Status, &scenario.Enabled, &scenario.Author, &scenario.Approver, &scenario.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(params, &scenario.Params)
		out = append(out, scenario)
	}
	return out, rows.Err()
}

func nullText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
