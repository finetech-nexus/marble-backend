package bankaml

import (
	"context"
	"fmt"
	"time"

	"github.com/checkmarble/marble-backend/models"
	"github.com/google/uuid"
)

func (s *Service) StartBatch(ctx context.Context, creds models.Credentials, mode string, clients []ClientInput, chunk int) (Batch, error) {
	if err := s.require(creds, ActBatch); err != nil {
		return Batch{}, err
	}
	switch mode {
	case "initial", "periodic", "since", "errors", "rules":
	default:
		return Batch{}, fmt.Errorf("unknown batch mode")
	}
	if _, running, err := s.Repo.RunningBatch(ctx, creds.OrganizationId.String()); err != nil {
		return Batch{}, err
	} else if running {
		return Batch{}, errConflict
	}
	selected := selectPopulation(mode, clients)
	if chunk <= 0 {
		chunk = 25
	}
	now := s.now()
	batch := Batch{
		ID: uuid.NewString(), OrgID: creds.OrganizationId.String(), Mode: mode, Status: BatchRunning,
		Population: selected, ChunkSize: chunk, Total: len(selected), Remaining: len(selected),
		TriggeredBy: actorOf(creds), StartedAt: &now, CreatedAt: now,
	}
	if err := s.Repo.SaveBatch(ctx, batch); err != nil {
		return Batch{}, err
	}
	go s.processBatch(context.Background(), batch.OrgID, batch.ID)
	batch.Population = nil
	return batch, nil
}

func selectPopulation(mode string, clients []ClientInput) []ClientInput {
	if mode != "since" && mode != "errors" {
		return clients
	}
	out := []ClientInput{}
	for _, client := range clients {
		if mode == "since" && client.Modified {
			out = append(out, client)
		}
		if mode == "errors" && client.LastError {
			out = append(out, client)
		}
	}
	return out
}

func (s *Service) ResumeBatch(ctx context.Context, creds models.Credentials, id string) (Batch, error) {
	if err := s.require(creds, ActBatch); err != nil {
		return Batch{}, err
	}
	batch, err := s.Repo.GetBatch(ctx, creds.OrganizationId.String(), id)
	if err != nil {
		return Batch{}, err
	}
	if batch.Status == BatchRunning || batch.Status == BatchPending {
		return Batch{}, errConflict
	}
	if batch.Cursor >= batch.Total {
		return batch, nil
	}
	batch.Status = BatchRunning
	batch.FinishedAt = nil
	if err := s.Repo.SaveBatch(ctx, batch); err != nil {
		return Batch{}, err
	}
	go s.processBatch(context.Background(), batch.OrgID, batch.ID)
	batch.Population = nil
	return batch, nil
}

func (s *Service) processBatch(ctx context.Context, org, id string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	batch, err := s.Repo.GetBatch(ctx, org, id)
	if err != nil {
		return
	}
	for batch.Cursor < len(batch.Population) {
		end := batch.Cursor + batch.ChunkSize
		if end > len(batch.Population) {
			end = len(batch.Population)
		}
		for _, client := range batch.Population[batch.Cursor:end] {
			item := BatchItem{ID: uuid.NewString(), BatchID: batch.ID, ClientRef: client.ClientRef, Status: "ok"}
			query := SearchQuery{
				ClientRef: client.ClientRef, EntityType: client.EntityType, Name: client.Name,
				Alias: client.Alias, BirthDate: client.BirthDate, Nationality: client.Nationality,
				Country: client.Country, Identifiers: client.Identifiers, Force: true,
			}
			orgID, parseErr := uuid.Parse(org)
			if parseErr != nil {
				item.Status = "error"
				item.Error = "invalid organization"
				batch.Failed++
				_ = s.Repo.AddBatchItem(ctx, org, item)
				batch.Cursor++
				continue
			}
			creds := models.Credentials{OrganizationId: orgID, ActorIdentity: models.Identity{Email: "batch:" + batch.TriggeredBy}, Roles: []models.Role{models.ADMIN}}
			_, matches, diff, searchErr := s.Search(ctx, creds, query)
			if searchErr != nil {
				item.Status = "error"
				item.Error = "screening failed"
				batch.Failed++
			} else {
				item.NewMatches = len(diff.New)
				item.ChangedMatches = len(diff.Changed)
				item.MissingMatches = len(diff.Missing)
				batch.Succeeded++
				batch.NewMatches += item.NewMatches
				batch.ChangedMatches += item.ChangedMatches
				batch.MissingMatches += item.MissingMatches
				_ = matches
			}
			_ = s.Repo.AddBatchItem(ctx, org, item)
			batch.Cursor++
		}
		batch.Remaining = batch.Total - batch.Cursor
		_ = s.Repo.SaveBatch(ctx, batch)
	}
	finished := s.now()
	batch.FinishedAt = &finished
	batch.Remaining = 0
	if batch.Failed > 0 && batch.Succeeded == 0 {
		batch.Status = BatchFailed
	} else if batch.Failed > 0 {
		batch.Status = BatchDoneErr
	} else {
		batch.Status = BatchDone
	}
	_ = s.Repo.SaveBatch(ctx, batch)
}
