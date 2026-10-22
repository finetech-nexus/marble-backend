package bankaml

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

func Mount(router gin.IRoutes, logger *slog.Logger) {
	repo, err := NewPG(context.Background())
	var store Repository
	if err != nil {
		logger.Warn("bank AML module has no database yet", "error", err.Error())
		store = NewMemory()
	} else {
		store = repo
	}
	handler := &Handler{Service: &Service{
		Repo:      store,
		Sanctions: sanctionsFromEnv(),
		Cooldown:  time.Minute,
	}}
	router.GET("/bank-aml/meta", handler.Meta)
	router.POST("/bank-aml/searches", handler.CreateSearch)
	router.GET("/bank-aml/searches", handler.ListSearches)
	router.GET("/bank-aml/searches/:id", handler.GetSearch)
	router.POST("/bank-aml/matches/:id/qualification", handler.Qualify)
	router.GET("/bank-aml/cases", handler.ListCases)
	router.GET("/bank-aml/cases/:id", handler.GetCase)
	router.POST("/bank-aml/cases/:id/transition", handler.Transition)
	router.POST("/bank-aml/cases/:id/assign", handler.Assign)
	router.POST("/bank-aml/batches", handler.StartBatch)
	router.GET("/bank-aml/batches", handler.ListBatches)
	router.GET("/bank-aml/batches/:id", handler.GetBatch)
	router.POST("/bank-aml/batches/:id/resume", handler.ResumeBatch)
	router.POST("/bank-aml/scores", handler.Calculate)
	router.GET("/bank-aml/scores", handler.ListScores)
	router.GET("/bank-aml/formulas", handler.ListFormulas)
	router.POST("/bank-aml/formulas", handler.SaveFormula)
	router.POST("/bank-aml/formulas/:id/approve", handler.ApproveFormula)
	router.GET("/bank-aml/scenarios", handler.ListScenarios)
	router.POST("/bank-aml/scenario-evaluations", handler.Evaluate)
	router.POST("/bank-aml/scenarios/:code", handler.SaveScenario)
	router.POST("/bank-aml/scenarios/:code/approve", handler.ApproveScenario)
	router.GET("/bank-aml/settings", handler.ListSettings)
	router.POST("/bank-aml/settings", handler.SaveSetting)
	router.POST("/bank-aml/settings/:id/approve", handler.ApproveSetting)
	router.GET("/bank-aml/audit", handler.Audit)
}

func sanctionsFromEnv() *OpenSanctions {
	host := firstEnv("SCREENING_OPENSANCTIONS_API_HOST", "OPENSANCTIONS_API_HOST")
	if host == "" {
		host = "https://api.opensanctions.org"
	}
	return &OpenSanctions{
		Host:       host,
		APIKey:     firstEnv("SCREENING_OPENSANCTIONS_API_KEY", "OPENSANCTIONS_API_KEY"),
		AuthMethod: firstEnv("SCREENING_OPENSANCTIONS_AUTH_METHOD", "OPENSANCTIONS_AUTH_METHOD"),
		Scope:      firstEnv("SCREENING_OPENSANCTIONS_SCOPE", "OPENSANCTIONS_SCOPE"),
		MinGap:     200 * time.Millisecond,
		Retries:    2,
	}
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}
