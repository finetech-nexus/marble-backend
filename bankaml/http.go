package bankaml

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/checkmarble/marble-backend/models"
	"github.com/checkmarble/marble-backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	Service *Service
}

func (h *Handler) creds(c *gin.Context) (models.Credentials, bool) {
	creds, ok := utils.CredentialsFromCtx(c.Request.Context())
	if !ok || creds.OrganizationId == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return models.Credentials{}, false
	}
	return creds, true
}

func (h *Handler) fail(c *gin.Context, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, errDenied):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, errNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, errConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "a batch is already running"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

func (h *Handler) Meta(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	actions := []Action{ActRead, ActSearch, ActQualify, ActBatch, ActSettings, ActApprove, ActScenario, ActActivate, ActScore, ActAssign, ActValidate, ActClose, ActAudit}
	allowed := map[string]bool{}
	for _, action := range actions {
		allowed[string(action)] = Allowed(creds, action)
	}
	c.JSON(http.StatusOK, gin.H{
		"permissions": allowed,
		"qualifications": []gin.H{
			{"code": QualToReview, "label": "À examiner"},
			{"code": QualInAnalysis, "label": "En cours d'analyse"},
			{"code": QualDismissed, "label": "Correspondance écartée"},
			{"code": QualConfirmed, "label": "Correspondance confirmée après examen"},
			{"code": QualEscalate, "label": "À escalader"},
			{"code": QualError, "label": "Erreur ou résultat indisponible"},
		},
		"case_statuses": []string{CaseNew, CaseToQualify, CaseInvestigating, CaseWaiting, CaseEscalated, CaseSecondLevel, CaseValidated, CaseClosed},
		"transmission": "Client search fields are sent to the OpenSanctions hosted match API. The API key is not returned.",
	})
}

func (h *Handler) CreateSearch(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var query SearchQuery
	if err := c.ShouldBindJSON(&query); err != nil {
		h.fail(c, err)
		return
	}
	search, matches, diff, err := h.Service.Search(c.Request.Context(), creds, query)
	if err != nil && search.ID == "" {
		h.fail(c, err)
		return
	}
	status := http.StatusOK
	if err != nil {
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{"search": search, "matches": matches, "diff": diff, "error": errString(err)})
}

func (h *Handler) ListSearches(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	if err := h.Service.require(creds, ActRead); err != nil {
		h.fail(c, err)
		return
	}
	items, err := h.Service.Repo.ListSearches(c.Request.Context(), creds.OrganizationId.String(), 50)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"searches": items})
}

func (h *Handler) GetSearch(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	search, matches, err := h.Service.Repo.GetSearch(c.Request.Context(), creds.OrganizationId.String(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"search": search, "matches": matches})
}

func (h *Handler) Qualify(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	match, err := h.Service.Qualify(c.Request.Context(), creds, c.Param("id"), body.Status, body.Reason)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, match)
}

func (h *Handler) ListCases(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	items, err := h.Service.Repo.ListCases(c.Request.Context(), creds.OrganizationId.String())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"cases": items})
}

func (h *Handler) GetCase(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	item, err := h.Service.Repo.GetCase(c.Request.Context(), creds.OrganizationId.String(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	item.Events, _ = h.Service.Repo.ListEvents(c.Request.Context(), item.OrgID, "case", item.ID)
	c.JSON(http.StatusOK, item)
}

func (h *Handler) Transition(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	item, err := h.Service.Transition(c.Request.Context(), creds, c.Param("id"), body.Status, body.Reason)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) Assign(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var body struct {
		Assignee string `json:"assignee"`
		Team     string `json:"team"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	item, err := h.Service.Assign(c.Request.Context(), creds, c.Param("id"), body.Assignee, body.Team)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) StartBatch(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var body struct {
		Mode      string        `json:"mode"`
		ChunkSize int           `json:"chunk_size"`
		Clients   []ClientInput `json:"clients"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	batch, err := h.Service.StartBatch(c.Request.Context(), creds, body.Mode, body.Clients, body.ChunkSize)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, batch)
}

func (h *Handler) ListBatches(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	items, err := h.Service.Repo.ListBatches(c.Request.Context(), creds.OrganizationId.String())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"batches": items})
}

func (h *Handler) GetBatch(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	item, err := h.Service.Repo.GetBatch(c.Request.Context(), creds.OrganizationId.String(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	item.Population = nil
	c.JSON(http.StatusOK, item)
}

func (h *Handler) ResumeBatch(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	item, err := h.Service.ResumeBatch(c.Request.Context(), creds, c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, item)
}

func (h *Handler) Calculate(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var body struct {
		Client  ClientInput `json:"client"`
		Matches []Match     `json:"matches"`
		Reason  string      `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	score, err := h.Service.Calculate(c.Request.Context(), creds, body.Client, body.Matches, body.Reason)
	if err != nil {
		h.fail(c, err)
		return
	}
	formula, _ := h.Service.Repo.ActiveFormula(c.Request.Context(), creds.OrganizationId.String())
	c.JSON(http.StatusOK, gin.H{"score": score, "class": ClassOf(score.Total, formula.Classes)})
}

func (h *Handler) ListScores(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	items, err := h.Service.Repo.ListScores(c.Request.Context(), creds.OrganizationId.String(), c.Query("client"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"scores": items})
}

func (h *Handler) ListFormulas(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	items, err := h.Service.Repo.ListFormulas(c.Request.Context(), creds.OrganizationId.String())
	if err != nil {
		h.fail(c, err)
		return
	}
	if len(items) == 0 {
		items = []Formula{DefaultFormula()}
	}
	c.JSON(http.StatusOK, gin.H{"formulas": items})
}

func (h *Handler) SaveFormula(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var formula Formula
	if err := c.ShouldBindJSON(&formula); err != nil {
		h.fail(c, err)
		return
	}
	saved, err := h.Service.SaveFormula(c.Request.Context(), creds, formula)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) ApproveFormula(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	saved, err := h.Service.ApproveFormula(c.Request.Context(), creds, c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) ListScenarios(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	items, err := h.Service.ensureScenarios(c.Request.Context(), creds.OrganizationId.String())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"scenarios": items, "execution": "complementary bankaml module"})
}

func (h *Handler) SaveScenario(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var scenario Scenario
	if err := c.ShouldBindJSON(&scenario); err != nil {
		h.fail(c, err)
		return
	}
	scenario.Code = c.Param("code")
	saved, err := h.Service.SaveScenarioDraft(c.Request.Context(), creds, scenario)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) ApproveScenario(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	version, _ := strconv.Atoi(c.Query("version"))
	saved, err := h.Service.ApproveScenario(c.Request.Context(), creds, c.Param("code"), version, c.Query("enabled") != "false")
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) Evaluate(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var body struct {
		Population Population `json:"population"`
		Commit     bool       `json:"commit"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	alerts, err := h.Service.Evaluate(c.Request.Context(), creds, body.Population, body.Commit)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"alerts": alerts})
}

func (h *Handler) ListSettings(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	items, err := h.Service.Repo.ListSettings(c.Request.Context(), creds.OrganizationId.String())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": items})
}

func (h *Handler) SaveSetting(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	var setting Setting
	if err := c.ShouldBindJSON(&setting); err != nil {
		h.fail(c, err)
		return
	}
	saved, err := h.Service.SaveSetting(c.Request.Context(), creds, setting)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) ApproveSetting(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	saved, err := h.Service.ApproveSetting(c.Request.Context(), creds, c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) Audit(c *gin.Context) {
	creds, ok := h.creds(c)
	if !ok {
		return
	}
	if err := h.Service.require(creds, ActAudit); err != nil {
		h.fail(c, err)
		return
	}
	items, err := h.Service.Repo.ListEvents(c.Request.Context(), creds.OrganizationId.String(), c.Query("type"), c.Query("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": items})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
