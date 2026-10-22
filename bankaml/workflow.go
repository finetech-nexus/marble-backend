package bankaml

import "fmt"

var transitions = map[string][]string{
	CaseNew:          {CaseToQualify, CaseInvestigating},
	CaseToQualify:    {CaseInvestigating, CaseEscalated, CaseWaiting},
	CaseInvestigating: {CaseWaiting, CaseEscalated, CaseSecondLevel, CaseValidated},
	CaseWaiting:      {CaseInvestigating},
	CaseEscalated:    {CaseSecondLevel, CaseInvestigating},
	CaseSecondLevel:  {CaseValidated, CaseInvestigating},
	CaseValidated:    {CaseClosed},
	CaseClosed:       {CaseInvestigating},
}

func CanTransition(from, to string) bool {
	for _, candidate := range transitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

type TransitionInput struct {
	To     string
	Actor  string
	Reason string
	Case   Case
}

func ApplyTransition(in TransitionInput) (Case, error) {
	if !CanTransition(in.Case.Status, in.To) {
		return Case{}, fmt.Errorf("transition %s -> %s is not allowed", in.Case.Status, in.To)
	}
	if needsReason(in.To) && in.Reason == "" {
		return Case{}, fmt.Errorf("a reason is required")
	}
	next := in.Case
	switch in.To {
	case CaseInvestigating:
		if in.Case.Status == CaseClosed && in.Reason == "" {
			return Case{}, fmt.Errorf("a reason is required")
		}
		if next.Investigator == "" {
			next.Investigator = in.Actor
		}
	case CaseValidated:
		if in.Case.Status == CaseSecondLevel {
			if in.Case.Level1Actor != "" && in.Case.Level1Actor == in.Actor {
				return Case{}, fmt.Errorf("second-level validation must be done by a different user")
			}
			next.Level2Actor = in.Actor
		} else {
			if in.Case.Investigator != "" && in.Case.Investigator == in.Actor {
				return Case{}, fmt.Errorf("first-level validation must be done by a different user")
			}
			next.Level1Actor = in.Actor
		}
	case CaseClosed:
		if in.Case.Level2Actor != "" && in.Case.Level2Actor == in.Actor {
			return Case{}, fmt.Errorf("closure must be done by a different user from the second-level reviewer")
		}
		if in.Case.Level1Actor != "" && in.Case.Level1Actor == in.Actor && in.Case.Level2Actor == "" {
			return Case{}, fmt.Errorf("closure must be done by a different user from the first-level reviewer")
		}
		next.ClosureReason = in.Reason
	}
	next.Status = in.To
	return next, nil
}

func needsReason(to string) bool {
	return to == CaseClosed || to == CaseEscalated || to == CaseValidated
}
