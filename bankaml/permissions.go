package bankaml

import "github.com/checkmarble/marble-backend/models"

type Action string

const (
	ActSearch   Action = "search"
	ActQualify  Action = "qualify"
	ActBatch    Action = "batch"
	ActSettings Action = "settings"
	ActApprove  Action = "approve"
	ActScenario Action = "scenario"
	ActActivate Action = "activate"
	ActScore    Action = "score"
	ActAssign   Action = "assign"
	ActValidate Action = "validate"
	ActClose    Action = "close"
	ActAudit    Action = "audit"
	ActRead     Action = "read"
)

func Allowed(creds models.Credentials, action Action) bool {
	switch action {
	case ActRead:
		return has(creds, models.VIEWER, models.ANALYST, models.BUILDER, models.PUBLISHER, models.ADMIN, models.MARBLE_ADMIN, models.TENANT_ADMIN)
	case ActSearch, ActQualify, ActValidate:
		return has(creds, models.ANALYST, models.PUBLISHER, models.ADMIN, models.MARBLE_ADMIN, models.TENANT_ADMIN)
	case ActScenario:
		return has(creds, models.BUILDER, models.PUBLISHER, models.ADMIN, models.MARBLE_ADMIN, models.TENANT_ADMIN)
	case ActApprove, ActActivate, ActClose, ActAudit, ActAssign:
		return has(creds, models.PUBLISHER, models.ADMIN, models.MARBLE_ADMIN, models.TENANT_ADMIN)
	case ActBatch, ActSettings, ActScore:
		return has(creds, models.ADMIN, models.MARBLE_ADMIN, models.TENANT_ADMIN)
	default:
		return false
	}
}

func has(creds models.Credentials, roles ...models.Role) bool {
	for _, role := range roles {
		if creds.HasRole(role) {
			return true
		}
	}
	return false
}

func actorOf(creds models.Credentials) string {
	if creds.ActorIdentity.Email != "" {
		return creds.ActorIdentity.Email
	}
	if creds.ActorIdentity.ApiKeyName != "" {
		return creds.ActorIdentity.ApiKeyName
	}
	return string(creds.ActorIdentity.UserId)
}
