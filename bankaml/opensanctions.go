package bankaml

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type OpenSanctions struct {
	Host       string
	APIKey     string
	AuthMethod string
	Scope      string
	Client     *http.Client
	MinGap     time.Duration
	Retries    int
	limiter    limiter
}

type limiter struct {
	mu   sync.Mutex
	next time.Time
	gap  time.Duration
}

func (l *limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	wait := time.Until(l.next)
	if wait < 0 {
		wait = 0
	}
	l.next = time.Now().Add(wait + l.gap)
	l.mu.Unlock()
	if wait == 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (o *OpenSanctions) Match(ctx context.Context, query SearchQuery) ([]Match, error) {
	if o.APIKey == "" {
		return nil, fmt.Errorf("opensanctions api key is not configured")
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 20 * time.Second}
	}
	if o.Host == "" {
		o.Host = "https://api.opensanctions.org"
	}
	if o.Scope == "" {
		o.Scope = "default"
	}
	if o.Retries == 0 {
		o.Retries = 2
	}
	if o.MinGap == 0 {
		o.MinGap = 200 * time.Millisecond
	}
	o.limiter.gap = o.MinGap

	body, transmitted := requestBody(query)
	threshold := query.Threshold
	if threshold == 0 {
		threshold = 70
	}
	endpoint := fmt.Sprintf("%s/match/%s?algorithm=logic-v2&threshold=%.2f&cutoff=%.2f&limit=20",
		strings.TrimRight(o.Host, "/"), url.PathEscape(o.Scope), float64(threshold)/100, float64(threshold)/100)

	var last error
	for attempt := 0; attempt <= o.Retries; attempt++ {
		if err := o.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("content-type", "application/json")
		req.Header.Set("accept", "application/json")
		switch strings.ToLower(o.AuthMethod) {
		case "bearer":
			req.Header.Set("authorization", "Bearer "+o.APIKey)
		case "basic":
			req.SetBasicAuth(o.APIKey, "")
		default:
			req.Header.Set("authorization", "ApiKey "+o.APIKey)
		}
		resp, err := o.Client.Do(req)
		if err != nil {
			last = err
			continue
		}
		payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			last = fmt.Errorf("opensanctions status %d", resp.StatusCode)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
			}
			continue
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("opensanctions status %d", resp.StatusCode)
		}
		matches, err := parseMatchResponse(payload, query, transmitted)
		return matches, err
	}
	if last == nil {
		last = fmt.Errorf("opensanctions request failed")
	}
	return nil, last
}

func requestBody(query SearchQuery) ([]byte, []string) {
	props := map[string][]string{}
	fields := []string{}
	add := func(field, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		props[field] = append(props[field], value)
		fields = append(fields, field)
	}
	add("name", query.Name)
	add("alias", query.Alias)
	schema := query.EntityType
	if schema == "" {
		schema = "Person"
	}
	if schema == "Person" {
		add("birthDate", query.BirthDate)
		add("nationality", query.Nationality)
	}
	if schema == "Organization" || query.Country != "" {
		add("country", query.Country)
	}
	if len(query.Identifiers) > 0 {
		props["idNumber"] = query.Identifiers
		fields = append(fields, "idNumber")
	}
	payload := map[string]any{
		"queries": map[string]any{
			"q": map[string]any{"schema": schema, "properties": props},
		},
	}
	raw, _ := json.Marshal(payload)
	return raw, fields
}

func parseMatchResponse(raw []byte, query SearchQuery, fields []string) ([]Match, error) {
	var parsed struct {
		Responses map[string]struct {
			Results []struct {
				ID         string         `json:"id"`
				Caption    string         `json:"caption"`
				Schema     string         `json:"schema"`
				Datasets   []string       `json:"datasets"`
				Properties map[string]any `json:"properties"`
				Score      *float64       `json:"score"`
				Features   map[string]any `json:"features"`
				Match      *bool          `json:"match"`
			} `json:"results"`
		} `json:"responses"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("opensanctions response is not a match payload")
	}
	response, ok := parsed.Responses["q"]
	if !ok {
		return nil, fmt.Errorf("opensanctions response has no query result")
	}
	now := time.Now().UTC()
	matches := make([]Match, 0, len(response.Results))
	for _, result := range response.Results {
		if result.Match != nil && !*result.Match {
			continue
		}
		score := result.Score
		if score != nil {
			scaled := *score * 100
			score = &scaled
		}
		matches = append(matches, Match{
			EntityID:    result.ID,
			EntityName:  result.Caption,
			EntityType:  result.Schema,
			Datasets:    result.Datasets,
			Matched:     overlap(query, result.Properties),
			Mismatched:  diverge(query, result.Properties),
			Score:       score,
			Explanation: result.Features,
			ConsultedAt: now,
			Qualification: QualToReview,
			ClientRef:   query.ClientRef,
		})
	}
	return matches, nil
}

func overlap(query SearchQuery, properties map[string]any) map[string]any {
	out := map[string]any{}
	if query.Name != "" && hasText(properties, "name", query.Name) {
		out["name"] = query.Name
	}
	if query.BirthDate != "" && hasText(properties, "birthDate", query.BirthDate) {
		out["birthDate"] = query.BirthDate
	}
	if query.Nationality != "" && hasText(properties, "nationality", query.Nationality) {
		out["nationality"] = query.Nationality
	}
	return out
}

func diverge(query SearchQuery, properties map[string]any) map[string]any {
	out := map[string]any{}
	if query.BirthDate != "" && !hasText(properties, "birthDate", query.BirthDate) && properties["birthDate"] != nil {
		out["birthDate"] = properties["birthDate"]
	}
	if query.Nationality != "" && !hasText(properties, "nationality", query.Nationality) && properties["nationality"] != nil {
		out["nationality"] = properties["nationality"]
	}
	return out
}

func hasText(properties map[string]any, key, want string) bool {
	raw, ok := properties[key]
	if !ok {
		return false
	}
	want = strings.ToLower(want)
	switch value := raw.(type) {
	case string:
		return strings.Contains(strings.ToLower(value), want)
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok && strings.Contains(strings.ToLower(text), want) {
				return true
			}
		}
	}
	return false
}

func Transmitted(query SearchQuery) Transmission {
	_, fields := requestBody(query)
	return Transmission{
		Destination: "OpenSanctions hosted match API",
		Fields:      fields,
		Note:        "Only the non-empty search fields are sent. The API key stays on the server and is not written to logs.",
	}
}
