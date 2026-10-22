package bankaml

import (
	"context"
	"fmt"
	"time"

	"github.com/checkmarble/marble-backend/models"
	"github.com/google/uuid"
)

func (s *Service) SaveSetting(ctx context.Context, creds models.Credentials, setting Setting) (Setting, error) {
	if err := s.require(creds, ActSettings); err != nil {
		return Setting{}, err
	}
	current, err := s.Repo.ListSettings(ctx, creds.OrganizationId.String())
	if err != nil {
		return Setting{}, err
	}
	version := 1
	for _, item := range current {
		if item.Key == setting.Key && item.Version >= version {
			version = item.Version + 1
		}
	}
	setting.ID = uuid.NewString()
	setting.OrgID = creds.OrganizationId.String()
	setting.Status = "pending_approval"
	setting.Version = version
	setting.Author = actorOf(creds)
	setting.CreatedAt = s.now()
	if err := s.Repo.SaveSetting(ctx, setting); err != nil {
		return Setting{}, err
	}
	return setting, nil
}

func (s *Service) ApproveSetting(ctx context.Context, creds models.Credentials, id string) (Setting, error) {
	if err := s.require(creds, ActApprove); err != nil {
		return Setting{}, err
	}
	settings, err := s.Repo.ListSettings(ctx, creds.OrganizationId.String())
	if err != nil {
		return Setting{}, err
	}
	for _, setting := range settings {
		if setting.ID != id {
			continue
		}
		if setting.Author == actorOf(creds) {
			return Setting{}, fmt.Errorf("the author cannot approve this version")
		}
		now := s.now()
		setting.Status = "active"
		setting.Approver = actorOf(creds)
		setting.EffectiveAt = &now
		if err := s.Repo.SaveSetting(ctx, setting); err != nil {
			return Setting{}, err
		}
		return setting, nil
	}
	return Setting{}, errNotFound
}

func (s *Service) SaveScenarioDraft(ctx context.Context, creds models.Credentials, scenario Scenario) (Scenario, error) {
	if err := s.require(creds, ActScenario); err != nil {
		return Scenario{}, err
	}
	current, err := s.ensureScenarios(ctx, creds.OrganizationId.String())
	if err != nil {
		return Scenario{}, err
	}
	version := 1
	for _, item := range current {
		if item.Code == scenario.Code && item.Version >= version {
			version = item.Version + 1
		}
	}
	scenario.OrgID = creds.OrganizationId.String()
	scenario.Version = version
	scenario.Status = "pending_approval"
	scenario.Enabled = false
	scenario.Author = actorOf(creds)
	scenario.UpdatedAt = s.now()
	if err := s.Repo.SaveScenario(ctx, scenario); err != nil {
		return Scenario{}, err
	}
	return scenario, nil
}

func (s *Service) ApproveScenario(ctx context.Context, creds models.Credentials, code string, version int, enable bool) (Scenario, error) {
	if err := s.require(creds, ActActivate); err != nil {
		return Scenario{}, err
	}
	scenarios, err := s.Repo.ListScenarios(ctx, creds.OrganizationId.String())
	if err != nil {
		return Scenario{}, err
	}
	for _, scenario := range scenarios {
		if scenario.Code != code || (version != 0 && scenario.Version != version) {
			continue
		}
		if scenario.Author == actorOf(creds) {
			return Scenario{}, fmt.Errorf("the author cannot approve this version")
		}
		scenario.Status = "active"
		scenario.Enabled = enable
		scenario.Approver = actorOf(creds)
		scenario.UpdatedAt = time.Now().UTC()
		if err := s.Repo.SaveScenario(ctx, scenario); err != nil {
			return Scenario{}, err
		}
		return scenario, nil
	}
	return Scenario{}, errNotFound
}
