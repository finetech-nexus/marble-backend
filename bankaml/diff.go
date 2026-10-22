package bankaml

func CompareMatches(previous, current []Match) DiffSummary {
	prev := map[string]Match{}
	for _, match := range previous {
		prev[match.EntityID] = match
	}
	seen := map[string]bool{}
	out := DiffSummary{
		Note: "A result missing from a later response is not proof that the entity left a sanctions list. It is kept for review.",
	}
	for _, match := range current {
		seen[match.EntityID] = true
		old, ok := prev[match.EntityID]
		if !ok {
			match.Change = "new"
			out.New = append(out.New, match)
			continue
		}
		if !sameMatch(old, match) {
			match.Change = "changed"
			out.Changed = append(out.Changed, match)
		}
	}
	for id, old := range prev {
		if !seen[id] {
			old.Change = "missing"
			out.Missing = append(out.Missing, old)
		}
	}
	return out
}

func sameMatch(a, b Match) bool {
	if a.EntityName != b.EntityName || a.EntityType != b.EntityType || len(a.Datasets) != len(b.Datasets) {
		return false
	}
	if (a.Score == nil) != (b.Score == nil) {
		return false
	}
	if a.Score != nil && b.Score != nil && *a.Score != *b.Score {
		return false
	}
	for i := range a.Datasets {
		if a.Datasets[i] != b.Datasets[i] {
			return false
		}
	}
	return true
}
