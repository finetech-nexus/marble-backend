package ai_agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/checkmarble/llmberjack"
	"github.com/checkmarble/llmberjack/llms/openai"
	"github.com/checkmarble/llmberjack/llms/perplexity"
	"github.com/checkmarble/marble-backend/models"
	"github.com/checkmarble/marble-backend/pure_utils"
	"github.com/checkmarble/marble-backend/utils"
	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
)

const ENRICHMENT_DEFAULT_MODEL = "sonar-pro"

const (
	INSTRUCTION_PATH   = models.AiAgentKYCInstructionPath
	PROMPT_ENRICH_PATH = models.AiAgentKYCPromptEnrichPath
)

var ErrKYCEnrichmentNotEnabled = errors.New("kyc enrichment is not enabled")

func (uc *AiAgentUsecase) getEnrichmentAdapter() (*llmberjack.Llmberjack, error) {
	if uc.enrichmentAdapter != nil {
		return uc.enrichmentAdapter, nil
	}

	if uc.config.PerplexityAPIKey == "" {
		return nil, errors.New("Perplexity API key is not configured")
	}

	opts := []openai.Opt{
		openai.WithApiKey(uc.config.PerplexityAPIKey),
	}

	provider, err := perplexity.New(opts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create Perplexity provider")
	}

	adapter, err := llmberjack.New(
		llmberjack.WithProvider("enrichment", provider),
		llmberjack.WithDefaultModel(ENRICHMENT_DEFAULT_MODEL),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create LLM adapter")
	}
	uc.enrichmentAdapter = adapter
	return uc.enrichmentAdapter, nil
}

type PivotDataForEnrichment struct {
	PivotObjectName string         `json:"pivot_object_name"`
	PivotData       map[string]any `json:"pivot_data"`
}

func (uc *AiAgentUsecase) EnrichCasePivotObjects(ctx context.Context, orgId uuid.UUID, caseId string) ([]models.AiEnrichmentKYC, error) {
	logger := utils.LoggerFromContext(ctx)

	// Get setting
	aiSetting, err := uc.getAiSetting(ctx, orgId)
	if err != nil {
		return nil, errors.Wrap(err, "could not get ai setting")
	}

	if !aiSetting.KYCEnrichmentSetting.Enabled {
		return nil, ErrKYCEnrichmentNotEnabled
	}

	// Get case data, included pivot data
	caseData, _, err := uc.getCaseDataWithPermissions(ctx, caseId)
	if err != nil {
		return nil, errors.Wrap(err, "could not get case with permissions")
	}

	pivotDataForEnrichment := make([]PivotDataForEnrichment, 0, len(caseData.pivotData))
	for _, pivotObject := range caseData.pivotData {
		pivotDataForEnrichment = append(pivotDataForEnrichment, PivotDataForEnrichment{
			PivotObjectName: pivotObject.PivotObjectName,
			PivotData:       pivotObject.PivotObjectData.Data,
		})
	}
	if len(pivotDataForEnrichment) == 0 {
		decisions, _, err := uc.repository.DecisionsByCaseIdFromCursor(ctx, uc.executorFactory.NewExecutor(), models.CaseDecisionsRequest{
			OrgId:  orgId,
			CaseId: caseId,
			Limit:  20,
		})
		if err != nil {
			return nil, errors.Wrap(err, "could not list case decisions for KYC enrichment")
		}
		for _, decision := range decisions {
			data := map[string]any{}
			for key, value := range decision.ClientObject.Data {
				data[key] = value
			}
			data["risk_score"] = decision.Score
			data["risk_level"] = decision.Outcome.String()
			tags := make([]string, 0, len(decision.ScreeningExecutions))
			for _, screening := range decision.ScreeningExecutions {
				if screening.Name != "" {
					tags = append(tags, screening.Name)
				}
			}
			if len(tags) > 0 {
				data["screening_tags"] = strings.Join(tags, ", ")
			}
			pivotDataForEnrichment = append(pivotDataForEnrichment, PivotDataForEnrichment{
				PivotObjectName: decision.ClientObject.TableName,
				PivotData:       data,
			})
		}
	}

	var results []models.AiEnrichmentKYC

	// For each pivot, do LLM request to enrich data
	// TODO: Parallelize this with ALL function from llmberjack
	for _, pivotObject := range pivotDataForEnrichment {
		logger.DebugContext(
			ctx,
			"KYC Enrichment - Pivot data for enrichment",
			"pivotDataForEnrichment",
			pivotObject,
		)
		pivotDataJson, err := json.Marshal(pivotObject)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal pivot data for enrichment")
		}
		result, err := uc.enrichData(ctx, pivotDataJson, aiSetting)
		if err != nil {
			return nil, errors.Wrap(err, "failed to enrich data")
		}

		results = append(results, result)
	}

	return results, nil
}

// Function to launch LLM request to enrich data
// data is JSON encoded which contains the data information like people name or company name
func (uc *AiAgentUsecase) enrichData(
	ctx context.Context,
	data []byte,
	aiSetting models.AiSetting,
) (models.AiEnrichmentKYC, error) {
	logger := utils.LoggerFromContext(ctx)

	// Get or initialize the enrichment adapter. Without a search provider, return the
	// prospect data already stored on the case so the reviewer still sees the KYC profile.
	adapter, err := uc.getEnrichmentAdapter()
	if err != nil {
		logger.InfoContext(ctx, "KYC enrichment uses stored prospect data", "error", err.Error())
		return localKYCEnrichment(data), nil
	}

	// For now, use the Language from the case review setting ... See if we need to refacto to move into a general setting
	language, err := pure_utils.BCP47ToEnglish(aiSetting.CaseReviewSetting.Language)
	if err != nil {
		return models.AiEnrichmentKYC{}, errors.Wrap(err,
			"failed to convert language to english")
	}

	// For INSTRUCTION_PATH, "custom_instructions" is an optional field that conditionally adds content to the instruction template.
	// If custom_instructions is empty/nil, exclude it from instructionData entirely to prevent the template's {{if}} condition from rendering the section.
	// Note: Including nil values in the data map results in "null" strings after JSON marshaling, which causes the template condition to evaluate as true.
	instructionData := map[string]any{
		"language": language,
	}
	if aiSetting.KYCEnrichmentSetting.CustomInstructions != nil &&
		*aiSetting.KYCEnrichmentSetting.CustomInstructions != "" {
		instructionData["custom_instructions"] = *aiSetting.KYCEnrichmentSetting.CustomInstructions
	}
	instruction, err := uc.preparePrompt(INSTRUCTION_PATH, instructionData)
	if err != nil {
		return models.AiEnrichmentKYC{}, errors.Wrap(err, "failed to read instruction")
	}
	prompt, err := uc.preparePrompt(PROMPT_ENRICH_PATH, map[string]any{
		"data": string(data),
	})
	if err != nil {
		return models.AiEnrichmentKYC{}, errors.Wrap(err, "failed to prepare request")
	}

	logger.DebugContext(ctx, "KYC Enrichment - Instruction", "instruction", instruction)
	logger.DebugContext(ctx, "KYC Enrichment - Prompt", "prompt", prompt)

	type output struct {
		Analysis   string `json:"analysis" jsonschema_description:"The complete, unmodified analysis with all details, formatting, and citations preserved exactly as you would normally provide"`
		EntityName string `json:"entity_name" jsonschema_description:"The name of the main entity analyzed"`
	}

	request := llmberjack.NewRequest[output]().
		WithInstruction(instruction).
		WithText(llmberjack.RoleUser, prompt)

	// Override the default model if set in the AI setting
	if aiSetting.KYCEnrichmentSetting.Model != nil {
		request = request.WithModel(*aiSetting.KYCEnrichmentSetting.Model)
	}

	// Perplexity specific options
	options := perplexity.RequestOptions{}
	if len(aiSetting.KYCEnrichmentSetting.DomainFilter) > 0 {
		options.SearchDomainFilter = aiSetting.KYCEnrichmentSetting.DomainFilter
	}
	request = request.WithProviderOptions(options)

	response, err := DoLLMRequest(ctx, adapter, request)
	if err != nil {
		logger.InfoContext(ctx, "KYC enrichment request failed, using stored prospect data", "error", err.Error())
		return localKYCEnrichment(data), nil
	}
	logger.DebugContext(ctx, "KYC Enrichment - Response", "response", response)

	var grounding []llmberjack.ResponseGroundingSource
	if len(response.Candidates) > 0 && response.Candidates[0].Grounding != nil {
		grounding = response.Candidates[0].Grounding.Sources
	}
	responseString, err := response.Get(0)
	if err != nil {
		return models.AiEnrichmentKYC{}, errors.Wrap(err, "failed to get response")
	}
	logger.DebugContext(ctx, "KYC Enrichment - Response", "responseString", responseString)

	return models.AiEnrichmentKYC{
		Analysis:   responseString.Analysis,
		EntityName: responseString.EntityName,
		Citations: pure_utils.Map(grounding, func(source llmberjack.ResponseGroundingSource) models.Citation {
			return models.Citation{
				Title:  source.Title,
				Domain: source.Domain,
				Url:    source.Url,
				Date:   source.Date,
			}
		}),
	}, nil
}

func localKYCEnrichment(data []byte) models.AiEnrichmentKYC {
	var payload PivotDataForEnrichment
	_ = json.Unmarshal(data, &payload)
	fields := payload.PivotData
	if fields == nil {
		fields = map[string]any{}
	}
	name := enrichmentText(fields, "full_name", "name", "legal_name")
	if name == "" {
		name = strings.TrimSpace(enrichmentText(fields, "first_name") + " " + enrichmentText(fields, "last_name"))
	}
	if name == "" {
		name = payload.PivotObjectName
	}
	yesNo := func(key string) string {
		value, ok := fields[key]
		if !ok {
			return "—"
		}
		if value == true || value == "true" {
			return "oui"
		}
		return "non"
	}
	analysis := fmt.Sprintf(`### Données déclarées
- Fonction publique : %s
- Proche d'une PPE : %s
- Implication financière avec une PPE : %s

### Données système
- Tags de screening : %s
- Score : %s
- Niveau : %s
- KYC : %s
- Expiration KYC : %s`,
		yesNo("pep_public_function"),
		yesNo("pep_family_associate"),
		yesNo("pep_financial_involvement"),
		orDash(enrichmentText(fields, "screening_tags")),
		orDash(enrichmentText(fields, "risk_score")),
		orDash(enrichmentText(fields, "risk_level")),
		orDash(enrichmentText(fields, "kyc_status")),
		orDash(enrichmentText(fields, "kyc_expires_at")),
	)
	return models.AiEnrichmentKYC{
		Analysis:   analysis,
		EntityName: name,
	}
}

func enrichmentText(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := fields[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		case float64:
			return fmt.Sprintf("%g", value)
		case int:
			return fmt.Sprintf("%d", value)
		case bool:
			if value {
				return "oui"
			}
			return "non"
		}
	}
	return ""
}

func orDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}
