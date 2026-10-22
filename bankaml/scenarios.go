package bankaml

import (
	"fmt"
	"math"
	"sort"
	"time"
)

func DefaultScenarios() []Scenario {
	note := "Example parameters for the bank to calibrate. They are not legal thresholds."
	return []Scenario{
		{Code: "UC01", Description: "Several operations of similar amounts whose sum exceeds a threshold in a time window. " + note, Params: map[string]any{"individual_min": 1000.0, "individual_max": 9000.0, "cumulative": 10000.0, "min_ops": 3.0, "window_hours": 24.0, "tolerance": 0.15}, Severity: "high", Priority: "high", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC02", Description: "Volume or frequency rises sharply versus the client's own history. " + note, Params: map[string]any{"baseline_days": 30.0, "observation_days": 7.0, "multiplier": 3.0, "min_volume": 5000.0, "min_ops": 5.0}, Severity: "medium", Priority: "medium", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC03", Description: "Incoming funds leave again quickly, with similar amounts. " + note, Params: map[string]any{"max_delay_hours": 48.0, "proportion": 0.8, "min_amount": 1000.0, "min_counterparties": 1.0}, Severity: "high", Priority: "high", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC04", Description: "Transfers that can return funds to their origin or a related account. " + note, Params: map[string]any{"depth": 4.0, "window_hours": 72.0, "tolerance": 0.1}, Severity: "high", Priority: "high", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC05", Description: "Cash deposits or withdrawals that are unusual for the client. " + note, Params: map[string]any{"min_amount": 2000.0, "min_ops": 3.0, "window_days": 7.0, "history_multiplier": 3.0}, Severity: "medium", Priority: "medium", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC06", Description: "Many counterparties, a sharp change of beneficiaries, or a concentrated flow. " + note, Params: map[string]any{"max_counterparties": 10.0, "concentration": 0.8, "window_days": 30.0}, Severity: "medium", Priority: "medium", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC07", Description: "Operations to or from countries on the bank's reference list. " + note, Params: map[string]any{"countries": []any{"KP", "IR"}}, Severity: "critical", Priority: "critical", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC08", Description: "Material activity after a configurable dormant period. " + note, Params: map[string]any{"inactive_days": 90.0, "min_amount": 1000.0}, Severity: "medium", Priority: "medium", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC09", Description: "Flows that depart from the activity declared in the KYC profile. " + note, Params: map[string]any{"volume_multiplier": 2.0, "count_multiplier": 2.0}, Severity: "medium", Priority: "medium", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC10", Description: "Shared counterparties or concentrations across several clients. " + note, Params: map[string]any{"min_clients": 3.0, "window_days": 30.0}, Severity: "high", Priority: "high", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC11", Description: "A new or changed potential screening match opens a review. A name similarity is not a confirmed sanction.", Params: map[string]any{}, Severity: "critical", Priority: "critical", Version: 1, Status: "draft", Enabled: false},
		{Code: "UC12", Description: "Recalculate the client score when a relevant event occurs and record why it changed.", Params: map[string]any{}, Severity: "medium", Priority: "medium", Version: 1, Status: "draft", Enabled: false},
	}
}

type Population struct {
	Clients      []ClientInput
	Transactions []Transaction
	Related      map[string][]string
	Now          time.Time
}

func EvaluateScenarios(scenarios []Scenario, pop Population) []Alert {
	if pop.Now.IsZero() {
		pop.Now = time.Now()
	}
	var alerts []Alert
	for _, scenario := range scenarios {
		if !scenario.Enabled || scenario.Status != "active" {
			continue
		}
		switch scenario.Code {
		case "UC01":
			alerts = append(alerts, uc01(scenario, pop)...)
		case "UC02":
			alerts = append(alerts, uc02(scenario, pop)...)
		case "UC03":
			alerts = append(alerts, uc03(scenario, pop)...)
		case "UC04":
			alerts = append(alerts, uc04(scenario, pop)...)
		case "UC05":
			alerts = append(alerts, uc05(scenario, pop)...)
		case "UC06":
			alerts = append(alerts, uc06(scenario, pop)...)
		case "UC07":
			alerts = append(alerts, uc07(scenario, pop)...)
		case "UC08":
			alerts = append(alerts, uc08(scenario, pop)...)
		case "UC09":
			alerts = append(alerts, uc09(scenario, pop)...)
		case "UC10":
			alerts = append(alerts, uc10(scenario, pop)...)
		}
	}
	return alerts
}

func ScreeningAlerts(scenario Scenario, diff DiffSummary) []Alert {
	if !scenario.Enabled || scenario.Status != "active" {
		return nil
	}
	var alerts []Alert
	for _, match := range append(append([]Match{}, diff.New...), diff.Changed...) {
		alerts = append(alerts, alert(scenario, match.ClientRef, "Potential screening match requires review. This is not a confirmed identity.", map[string]any{
			"entity_id": match.EntityID, "change": match.Change, "score": match.Score,
		}))
	}
	for _, match := range diff.Missing {
		alerts = append(alerts, alert(scenario, match.ClientRef, "A previously returned result was not returned again. This does not mean the entity is no longer sanctioned.", map[string]any{
			"entity_id": match.EntityID, "change": "missing",
		}))
	}
	return alerts
}

func uc01(s Scenario, pop Population) []Alert {
	minAmt, maxAmt := num(s.Params, "individual_min"), num(s.Params, "individual_max")
	cumulative, minOps := num(s.Params, "cumulative"), int(num(s.Params, "min_ops"))
	window := time.Duration(num(s.Params, "window_hours") * float64(time.Hour))
	tolerance := num(s.Params, "tolerance")
	byAccount := map[string][]Transaction{}
	for _, tx := range pop.Transactions {
		if tx.Amount < minAmt || tx.Amount > maxAmt {
			continue
		}
		byAccount[tx.AccountID] = append(byAccount[tx.AccountID], tx)
	}
	var alerts []Alert
	for account, txs := range byAccount {
		sort.Slice(txs, func(i, j int) bool { return txs[i].At.Before(txs[j].At) })
		for i := range txs {
			group := []Transaction{txs[i]}
			sum := txs[i].Amount
			for j := i + 1; j < len(txs); j++ {
				if txs[j].At.Sub(txs[i].At) > window {
					break
				}
				if math.Abs(txs[j].Amount-txs[i].Amount)/math.Max(txs[i].Amount, 1) > tolerance {
					continue
				}
				group = append(group, txs[j])
				sum += txs[j].Amount
			}
			if len(group) >= minOps && sum > cumulative {
				alerts = append(alerts, alert(s, txs[i].ClientID, fmt.Sprintf("%d similar operations on %s sum to %.2f", len(group), account, sum), map[string]any{"sum": sum, "count": len(group)}))
				break
			}
		}
	}
	return alerts
}

func uc02(s Scenario, pop Population) []Alert {
	base := time.Duration(num(s.Params, "baseline_days") * float64(24*time.Hour))
	obs := time.Duration(num(s.Params, "observation_days") * float64(24*time.Hour))
	mult, minVol, minOps := num(s.Params, "multiplier"), num(s.Params, "min_volume"), int(num(s.Params, "min_ops"))
	type agg struct{ volume float64; count int }
	baseAgg, obsAgg := map[string]agg{}, map[string]agg{}
	for _, tx := range pop.Transactions {
		age := pop.Now.Sub(tx.At)
		item := agg{volume: tx.Amount, count: 1}
		if age <= obs {
			cur := obsAgg[tx.ClientID]
			cur.volume += item.volume
			cur.count += item.count
			obsAgg[tx.ClientID] = cur
		} else if age <= base+obs {
			cur := baseAgg[tx.ClientID]
			cur.volume += item.volume
			cur.count += item.count
			baseAgg[tx.ClientID] = cur
		}
	}
	var alerts []Alert
	for client, observed := range obsAgg {
		baseline := baseAgg[client]
		if baseline.volume == 0 || observed.count < minOps || observed.volume < minVol {
			continue
		}
		if observed.volume > mult*baseline.volume || observed.count > int(mult*float64(baseline.count)) {
			alerts = append(alerts, alert(s, client, fmt.Sprintf("observed volume %.2f versus baseline %.2f", observed.volume, baseline.volume), map[string]any{"observed": observed.volume, "baseline": baseline.volume}))
		}
	}
	return alerts
}

func uc03(s Scenario, pop Population) []Alert {
	delay := time.Duration(num(s.Params, "max_delay_hours") * float64(time.Hour))
	proportion, minAmount := num(s.Params, "proportion"), num(s.Params, "min_amount")
	minCP := int(num(s.Params, "min_counterparties"))
	var alerts []Alert
	for _, inbound := range pop.Transactions {
		if inbound.Direction != "in" || inbound.Amount < minAmount {
			continue
		}
		out, parties := 0.0, map[string]bool{}
		for _, tx := range pop.Transactions {
			if tx.ClientID != inbound.ClientID || tx.Direction != "out" {
				continue
			}
			if tx.At.Before(inbound.At) || tx.At.Sub(inbound.At) > delay {
				continue
			}
			out += tx.Amount
			if tx.Counterparty != "" {
				parties[tx.Counterparty] = true
			}
		}
		if out >= proportion*inbound.Amount && len(parties) >= minCP {
			alerts = append(alerts, alert(s, inbound.ClientID, fmt.Sprintf("%.0f%% of incoming %.2f left within the delay", 100*out/inbound.Amount, inbound.Amount), map[string]any{"inbound": inbound.Amount, "outbound": out}))
			break
		}
	}
	return alerts
}

func uc04(s Scenario, pop Population) []Alert {
	depth := int(num(s.Params, "depth"))
	window := time.Duration(num(s.Params, "window_hours") * float64(time.Hour))
	tolerance := num(s.Params, "tolerance")
	type edge struct{ to string; amount float64; at time.Time; client string }
	graph := map[string][]edge{}
	for _, tx := range pop.Transactions {
		if tx.Direction != "out" || tx.CounterpartyAccount == "" {
			continue
		}
		graph[tx.AccountID] = append(graph[tx.AccountID], edge{tx.CounterpartyAccount, tx.Amount, tx.At, tx.ClientID})
	}
	var alerts []Alert
	seen := map[string]bool{}
	for origin, edges := range graph {
		related := map[string]bool{origin: true}
		for _, id := range pop.Related[origin] {
			related[id] = true
		}
		var walk func(node string, startAmt float64, start time.Time, left int, path []string)
		walk = func(node string, startAmt float64, start time.Time, left int, path []string) {
			if left == 0 {
				return
			}
			for _, next := range graph[node] {
				if next.at.Sub(start) > window || math.Abs(next.amount-startAmt)/math.Max(startAmt, 1) > tolerance {
					continue
				}
				if related[next.to] && len(path) > 0 {
					key := origin + ":" + next.client
					if !seen[key] {
						seen[key] = true
						alerts = append(alerts, alert(s, next.client, "Funds can return to the origin or a related account.", map[string]any{"origin": origin, "path": append(path, next.to)}))
					}
					return
				}
				walk(next.to, startAmt, start, left-1, append(path, next.to))
			}
		}
		for _, first := range edges {
			walk(first.to, first.amount, first.at, depth-1, []string{origin, first.to})
		}
	}
	return alerts
}

func uc05(s Scenario, pop Population) []Alert {
	window := time.Duration(num(s.Params, "window_days") * float64(24*time.Hour))
	minAmount, minOps := num(s.Params, "min_amount"), int(num(s.Params, "min_ops"))
	mult := num(s.Params, "history_multiplier")
	recent, older := map[string]float64{}, map[string]float64{}
	counts := map[string]int{}
	for _, tx := range pop.Transactions {
		if tx.Channel != "cash" {
			continue
		}
		if pop.Now.Sub(tx.At) <= window {
			recent[tx.ClientID] += tx.Amount
			counts[tx.ClientID]++
		} else {
			older[tx.ClientID] += tx.Amount
		}
	}
	var alerts []Alert
	for client, amount := range recent {
		if counts[client] >= minOps && amount >= minAmount && (older[client] == 0 || amount > mult*older[client]) {
			alerts = append(alerts, alert(s, client, fmt.Sprintf("cash amount %.2f over %d operations", amount, counts[client]), map[string]any{"amount": amount}))
		}
	}
	return alerts
}

func uc06(s Scenario, pop Population) []Alert {
	window := time.Duration(num(s.Params, "window_days") * float64(24*time.Hour))
	maxCP, concentration := int(num(s.Params, "max_counterparties")), num(s.Params, "concentration")
	parties := map[string]map[string]float64{}
	for _, tx := range pop.Transactions {
		if tx.Counterparty == "" || pop.Now.Sub(tx.At) > window {
			continue
		}
		if parties[tx.ClientID] == nil {
			parties[tx.ClientID] = map[string]float64{}
		}
		parties[tx.ClientID][tx.Counterparty] += tx.Amount
	}
	var alerts []Alert
	for client, flows := range parties {
		total := 0.0
		top := 0.0
		for _, amount := range flows {
			total += amount
			if amount > top {
				top = amount
			}
		}
		if len(flows) > maxCP || (total > 0 && top/total >= concentration) {
			alerts = append(alerts, alert(s, client, fmt.Sprintf("%d counterparties, top share %.0f%%", len(flows), 100*top/math.Max(total, 1)), map[string]any{"counterparties": len(flows)}))
		}
	}
	return alerts
}

func uc07(s Scenario, pop Population) []Alert {
	countries := map[string]bool{}
	switch raw := s.Params["countries"].(type) {
	case []any:
		for _, item := range raw {
			if text, ok := item.(string); ok {
				countries[text] = true
			}
		}
	case []string:
		for _, item := range raw {
			countries[item] = true
		}
	}
	var alerts []Alert
	seen := map[string]bool{}
	for _, tx := range pop.Transactions {
		if countries[tx.Country] && !seen[tx.ClientID+"/"+tx.Country] {
			seen[tx.ClientID+"/"+tx.Country] = true
			alerts = append(alerts, alert(s, tx.ClientID, "Operation involves a country on the bank reference list: "+tx.Country, map[string]any{"country": tx.Country, "transaction_id": tx.ID}))
		}
	}
	return alerts
}

func uc08(s Scenario, pop Population) []Alert {
	inactive := time.Duration(num(s.Params, "inactive_days") * float64(24*time.Hour))
	minAmount := num(s.Params, "min_amount")
	last := map[string]time.Time{}
	var ordered []Transaction
	ordered = append(ordered, pop.Transactions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].At.Before(ordered[j].At) })
	var alerts []Alert
	hit := map[string]bool{}
	for _, tx := range ordered {
		if prev, ok := last[tx.ClientID]; ok && tx.At.Sub(prev) >= inactive && tx.Amount >= minAmount && !hit[tx.ClientID] {
			hit[tx.ClientID] = true
			alerts = append(alerts, alert(s, tx.ClientID, fmt.Sprintf("activity resumed after %d days", int(tx.At.Sub(prev).Hours()/24)), map[string]any{"amount": tx.Amount}))
		}
		last[tx.ClientID] = tx.At
	}
	return alerts
}

func uc09(s Scenario, pop Population) []Alert {
	volMult, countMult := num(s.Params, "volume_multiplier"), num(s.Params, "count_multiplier")
	profiles := map[string]ClientProfile{}
	for _, client := range pop.Clients {
		profiles[client.ClientRef] = client.Profile
	}
	volume, count := map[string]float64{}, map[string]int{}
	for _, tx := range pop.Transactions {
		volume[tx.ClientID] += tx.Amount
		count[tx.ClientID]++
	}
	var alerts []Alert
	for client, profile := range profiles {
		if profile.ExpectedMonthlyVolume > 0 && volume[client] > volMult*profile.ExpectedMonthlyVolume {
			alerts = append(alerts, alert(s, client, fmt.Sprintf("volume %.2f exceeds the KYC expectation %.2f", volume[client], profile.ExpectedMonthlyVolume), map[string]any{"volume": volume[client]}))
		} else if profile.ExpectedMonthlyCount > 0 && float64(count[client]) > countMult*float64(profile.ExpectedMonthlyCount) {
			alerts = append(alerts, alert(s, client, "operation count exceeds the KYC expectation", map[string]any{"count": count[client]}))
		}
	}
	return alerts
}

func uc10(s Scenario, pop Population) []Alert {
	window := time.Duration(num(s.Params, "window_days") * float64(24*time.Hour))
	minClients := int(num(s.Params, "min_clients"))
	shared := map[string]map[string]bool{}
	for _, tx := range pop.Transactions {
		if tx.Counterparty == "" || pop.Now.Sub(tx.At) > window {
			continue
		}
		if shared[tx.Counterparty] == nil {
			shared[tx.Counterparty] = map[string]bool{}
		}
		shared[tx.Counterparty][tx.ClientID] = true
	}
	var alerts []Alert
	seen := map[string]bool{}
	for party, clients := range shared {
		if len(clients) < minClients {
			continue
		}
		for client := range clients {
			if seen[client] {
				continue
			}
			seen[client] = true
			alerts = append(alerts, alert(s, client, "Counterparty "+party+" is shared with other clients.", map[string]any{"counterparty": party, "clients": len(clients)}))
		}
	}
	return alerts
}

func alert(s Scenario, client, explanation string, evidence map[string]any) Alert {
	return Alert{
		Code: s.Code, ClientID: client, Severity: s.Severity, Priority: s.Priority,
		DedupKey: fmt.Sprintf("%s:%s:%d", s.Code, client, s.Version),
		Explanation: explanation, Version: fmt.Sprintf("%d", s.Version), Evidence: evidence,
	}
}

func num(params map[string]any, key string) float64 {
	switch value := params[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return 0
	}
}
