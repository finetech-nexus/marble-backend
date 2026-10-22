package bankaml

import "fmt"

func DefaultFormula() Formula {
	return Formula{
		Weights: Weights{KYC: 0.35, Transactions: 0.40, Alerts: 0.25},
		Classes: map[string]float64{"low": 30, "moderate": 60, "high": 85},
		Factors: []Factor{
			{Code: "client_type", Component: "kyc", Source: "profile.type", Rule: "company adds 25, other adds 5", Weight: 0.25, Version: 1, Justification: "Initial proposal, not a regulatory model."},
			{Code: "country", Component: "kyc", Source: "profile.country", Rule: "configured high-risk country adds 40", Weight: 0.40, Version: 1, Justification: "Bank reference list, not a legal list."},
			{Code: "kyc_age", Component: "kyc", Source: "profile.kyc_updated_at", Rule: "older than configured days adds 20", Weight: 0.20, Version: 1, Justification: "Data quality signal."},
			{Code: "tx_volume", Component: "transactions", Source: "transactions", Rule: "volume above expected monthly volume", Weight: 0.6, Version: 1, Justification: "Behaviour versus declared activity."},
			{Code: "tx_frequency", Component: "transactions", Source: "transactions", Rule: "count above expected monthly count", Weight: 0.4, Version: 1, Justification: "Behaviour versus declared activity."},
			{Code: "open_alerts", Component: "alerts", Source: "investigations", Rule: "open alerts add up to 100", Weight: 1, Version: 1, Justification: "Investigation history. Sanctions stay on a separate status."},
		},
		Description: "Initial weights KYC 35%, transactions 40%, alerts 25%. Not a validated risk model and not a regulatory standard.",
		Status:      "active",
		Version:     1,
		Author:      "system",
	}
}

func ValidateFormula(f Formula) error {
	sum := f.Weights.KYC + f.Weights.Transactions + f.Weights.Alerts
	if sum < 0.999 || sum > 1.001 {
		return fmt.Errorf("weights must sum to 1")
	}
	for _, w := range []float64{f.Weights.KYC, f.Weights.Transactions, f.Weights.Alerts} {
		if w < 0 || w > 1 {
			return fmt.Errorf("weights must be between 0 and 1")
		}
	}
	low, mod, high := f.Classes["low"], f.Classes["moderate"], f.Classes["high"]
	if !(low < mod && mod < high && high <= 100 && low > 0) {
		return fmt.Errorf("class thresholds must increase and stay inside 0..100")
	}
	return nil
}

type ScoreInput struct {
	Client          ClientInput
	HighRiskCountry map[string]bool
	KYCStaleDays    int
	Matches         []Match
	Reason          string
	Formula         Formula
}

func CalculateScore(in ScoreInput) Score {
	formula := in.Formula
	if formula.Version == 0 {
		formula = DefaultFormula()
	}
	missing := []string{}
	kycParts := []FactorResult{}
	kyc := 0.0
	if in.Client.KYCScore != nil {
		kyc = clamp(*in.Client.KYCScore)
		kycParts = append(kycParts, FactorResult{Code: "provided_kyc", Value: kyc, Note: "component supplied by the caller"})
	} else {
		typeScore := 5.0
		if in.Client.Profile.Type == "company" {
			typeScore = 25
		}
		kyc += typeScore
		kycParts = append(kycParts, FactorResult{Code: "client_type", Value: typeScore, Note: in.Client.Profile.Type})
		if in.Client.Profile.Country == "" {
			missing = append(missing, "profile.country")
		} else if in.HighRiskCountry[in.Client.Profile.Country] {
			kyc += 40
			kycParts = append(kycParts, FactorResult{Code: "country", Value: 40, Note: in.Client.Profile.Country})
		}
		if in.Client.Profile.KYCUpdatedAt.IsZero() {
			missing = append(missing, "profile.kyc_updated_at")
		}
	}
	kyc = clamp(kyc)

	volume := 0.0
	count := 0
	for _, tx := range in.Client.Transactions {
		volume += tx.Amount
		count++
	}
	txScore := 0.0
	txParts := []FactorResult{}
	if in.Client.Profile.ExpectedMonthlyVolume > 0 && volume > in.Client.Profile.ExpectedMonthlyVolume {
		ratio := volume / in.Client.Profile.ExpectedMonthlyVolume
		value := clamp((ratio - 1) * 50)
		txScore += value * 0.6
		txParts = append(txParts, FactorResult{Code: "tx_volume", Value: value, Note: fmt.Sprintf("volume %.2f vs expected %.2f", volume, in.Client.Profile.ExpectedMonthlyVolume)})
	}
	if in.Client.Profile.ExpectedMonthlyCount > 0 && count > in.Client.Profile.ExpectedMonthlyCount {
		value := clamp(float64(count-in.Client.Profile.ExpectedMonthlyCount) * 10)
		txScore += value * 0.4
		txParts = append(txParts, FactorResult{Code: "tx_frequency", Value: value, Note: fmt.Sprintf("%d operations", count)})
	}
	if len(in.Client.Transactions) == 0 {
		missing = append(missing, "transactions")
	}
	txScore = clamp(txScore)

	alertScore := clamp(float64(in.Client.OpenAlerts) * 25)
	alertParts := []FactorResult{{Code: "open_alerts", Value: alertScore, Note: fmt.Sprintf("%d open alerts", in.Client.OpenAlerts)}}

	total := formula.Weights.KYC*kyc + formula.Weights.Transactions*txScore + formula.Weights.Alerts*alertScore
	status, review := sanctionsStatus(in.Matches)
	return Score{
		ClientRef:       in.Client.ClientRef,
		Total:           total,
		Components:      map[string]float64{"kyc": kyc, "transactions": txScore, "alerts": alertScore},
		Factors:         append(append(kycParts, txParts...), alertParts...),
		FormulaVersion:  formula.Version,
		SanctionsStatus: status,
		ReviewRequired:  review,
		Missing:         missing,
		Reason:          in.Reason,
	}
}

func ClassOf(total float64, classes map[string]float64) string {
	if len(classes) == 0 {
		classes = DefaultFormula().Classes
	}
	switch {
	case total < classes["low"]:
		return "low"
	case total < classes["moderate"]:
		return "moderate"
	case total < classes["high"]:
		return "high"
	default:
		return "critical"
	}
}

func sanctionsStatus(matches []Match) (string, bool) {
	status := "clear"
	review := false
	for _, match := range matches {
		switch match.Qualification {
		case QualConfirmed:
			return "potential_sanctions_match", true
		case QualEscalate, QualToReview, QualInAnalysis:
			status = "potential_sanctions_match"
			review = true
		case QualError:
			if status != "potential_sanctions_match" {
				status = "screening_error"
			}
			review = true
		}
	}
	return status, review
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
