-- +goose Up

create table bankaml_settings (
  id uuid primary key,
  org_id uuid not null references organizations (id),
  key text not null,
  value jsonb not null,
  description text not null default '',
  status text not null,
  version int not null,
  effective_at timestamptz,
  author text not null,
  approver text,
  created_at timestamptz not null default now(),
  unique (org_id, key, version)
);

create table bankaml_searches (
  id uuid primary key,
  org_id uuid not null references organizations (id),
  client_ref text,
  query jsonb not null,
  query_hash text not null,
  source text not null default 'opensanctions',
  status text not null,
  error text,
  transmitted jsonb not null default '[]',
  created_by text not null,
  created_at timestamptz not null default now()
);

create index bankaml_searches_hash on bankaml_searches (org_id, query_hash, created_at desc);
create index bankaml_searches_client on bankaml_searches (org_id, client_ref, created_at desc);

create table bankaml_matches (
  id uuid primary key,
  org_id uuid not null references organizations (id),
  search_id uuid not null references bankaml_searches (id),
  client_ref text,
  entity_id text not null,
  entity_name text not null,
  entity_type text not null,
  datasets jsonb not null default '[]',
  matched jsonb not null default '{}',
  mismatched jsonb not null default '{}',
  score double precision,
  explanation jsonb not null default '{}',
  consulted_at timestamptz not null,
  qualification text not null default 'to_review',
  qualification_reason text,
  qualified_by text,
  qualified_at timestamptz,
  raw jsonb not null default '{}'
);

create index bankaml_matches_client on bankaml_matches (org_id, client_ref, entity_id);

create table bankaml_events (
  id uuid primary key,
  org_id uuid not null,
  object_type text not null,
  object_id text not null,
  action text not null,
  actor text not null,
  reason text,
  payload jsonb not null default '{}',
  created_at timestamptz not null default now()
);

create index bankaml_events_object on bankaml_events (org_id, object_type, object_id, created_at);

create table bankaml_cases (
  id uuid primary key,
  org_id uuid not null references organizations (id),
  alert_key text not null,
  client_ref text not null,
  status text not null,
  priority text not null default 'normal',
  assignee text,
  team text,
  due_at timestamptz,
  scenario_code text,
  alert jsonb not null default '{}',
  score jsonb,
  screening jsonb,
  closure_reason text,
  investigator text,
  level1_actor text,
  level2_actor text,
  opened_by text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (org_id, alert_key)
);

create table bankaml_batches (
  id uuid primary key,
  org_id uuid not null references organizations (id),
  mode text not null,
  status text not null,
  population jsonb not null,
  chunk_size int not null,
  cursor_pos int not null default 0,
  total_count int not null default 0,
  succeeded int not null default 0,
  failed int not null default 0,
  new_matches int not null default 0,
  changed_matches int not null default 0,
  missing_matches int not null default 0,
  remaining int not null default 0,
  triggered_by text not null,
  error text,
  started_at timestamptz,
  finished_at timestamptz,
  created_at timestamptz not null default now()
);

create index bankaml_batches_running on bankaml_batches (org_id) where status in ('pending', 'running');

create table bankaml_batch_items (
  id uuid primary key,
  batch_id uuid not null references bankaml_batches (id),
  org_id uuid not null,
  client_ref text not null,
  status text not null,
  error text,
  new_matches int not null default 0,
  changed_matches int not null default 0,
  missing_matches int not null default 0,
  created_at timestamptz not null default now()
);

create table bankaml_formulas (
  id uuid primary key,
  org_id uuid not null references organizations (id),
  weights jsonb not null,
  classes jsonb not null,
  factors jsonb not null,
  description text not null,
  status text not null,
  version int not null,
  author text not null,
  approver text,
  created_at timestamptz not null default now(),
  unique (org_id, version)
);

create table bankaml_scores (
  id uuid primary key,
  org_id uuid not null references organizations (id),
  client_ref text not null,
  total double precision not null,
  components jsonb not null,
  factors jsonb not null,
  formula_version int not null,
  sanctions_status text not null,
  review_required boolean not null,
  missing jsonb not null default '[]',
  reference_at timestamptz not null,
  reason text not null,
  created_by text not null,
  created_at timestamptz not null default now()
);

create index bankaml_scores_client on bankaml_scores (org_id, client_ref, created_at desc);

create table bankaml_scenarios (
  org_id uuid not null references organizations (id),
  code text not null,
  description text not null,
  params jsonb not null,
  severity text not null,
  priority text not null,
  version int not null,
  status text not null,
  enabled boolean not null default false,
  author text not null,
  approver text,
  updated_at timestamptz not null default now(),
  primary key (org_id, code, version)
);

-- +goose Down

drop table if exists bankaml_scenarios;
drop table if exists bankaml_scores;
drop table if exists bankaml_formulas;
drop table if exists bankaml_batch_items;
drop table if exists bankaml_batches;
drop table if exists bankaml_cases;
drop table if exists bankaml_events;
drop table if exists bankaml_matches;
drop table if exists bankaml_searches;
drop table if exists bankaml_settings;
