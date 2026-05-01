package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"stagehand/internal/services/stripe"
)

func runReferenceID(kind string, caseID string, now time.Time) string {
	clean := strings.NewReplacer(" ", "_", "/", "_", ".", "_").Replace(strings.ToLower(caseID))
	return fmt.Sprintf("conf_%s_%s_%d", kind, clean, now.UnixNano())
}

func sessionName(caseID string, kind string) string {
	clean := strings.NewReplacer(" ", "_", "/", "_", ".", "_").Replace(strings.ToLower(caseID))
	return "conf_" + clean + "_" + kind
}

func interactionID(prefix string, idx int) string {
	return fmt.Sprintf("int_%s_%03d", prefix, idx+1)
}

func decodeHTTPObservation(response *http.Response) (observation, error) {
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return observation{}, err
	}
	decoded := map[string]any{}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			decoded = map[string]any{"raw": string(body)}
		}
	}
	observed := observation{
		StatusCode: response.StatusCode,
		Response:   map[string]any{"body": decoded},
	}
	if response.StatusCode >= 400 {
		observed.Error = map[string]any{
			"status":      response.Status,
			"status_code": response.StatusCode,
			"body":        decoded,
		}
	}
	return observed, nil
}

func stripeEndpoint(step CaseStep) (string, string, error) {
	base := "https://api.stripe.com/v1"
	id := url.PathEscape(stringValue(step.Request["id"]))
	switch step.Operation {
	case stripe.OperationCustomersCreate:
		return http.MethodPost, base + "/customers", nil
	case stripe.OperationCustomersSearch:
		endpoint, err := url.Parse(base + "/customers/search")
		if err != nil {
			return "", "", err
		}
		query := endpoint.Query()
		query.Set("query", stringValue(step.Request["query"]))
		endpoint.RawQuery = query.Encode()
		return http.MethodGet, endpoint.String(), nil
	case stripe.OperationCustomersRetrieve:
		return http.MethodGet, base + "/customers/" + id, nil
	case stripe.OperationCustomersUpdate:
		return http.MethodPost, base + "/customers/" + id, nil
	case stripe.OperationPaymentMethodsCreate:
		return http.MethodPost, base + "/payment_methods", nil
	case stripe.OperationPaymentMethodsRetrieve:
		return http.MethodGet, base + "/payment_methods/" + id, nil
	case stripe.OperationPaymentMethodsUpdate:
		return http.MethodPost, base + "/payment_methods/" + id, nil
	case stripe.OperationPaymentMethodsAttach:
		return http.MethodPost, base + "/payment_methods/" + id + "/attach", nil
	case stripe.OperationPaymentIntentsCreate:
		return http.MethodPost, base + "/payment_intents", nil
	case stripe.OperationPaymentIntentsList:
		endpoint, err := url.Parse(base + "/payment_intents")
		if err != nil {
			return "", "", err
		}
		query := endpoint.Query()
		if customer := strings.TrimSpace(stringValue(step.Request["customer"])); customer != "" {
			query.Set("customer", customer)
		}
		if limit := int64Value(step.Request["limit"]); limit > 0 {
			query.Set("limit", strconv.FormatInt(limit, 10))
		}
		endpoint.RawQuery = query.Encode()
		return http.MethodGet, endpoint.String(), nil
	case stripe.OperationPaymentIntentsRetrieve:
		return http.MethodGet, base + "/payment_intents/" + id, nil
	case stripe.OperationPaymentIntentsUpdate:
		return http.MethodPost, base + "/payment_intents/" + id, nil
	case stripe.OperationRefundsCreate:
		return http.MethodPost, base + "/refunds", nil
	default:
		return "", "", fmt.Errorf("unsupported Stripe operation %q", step.Operation)
	}
}

func encodeStripeForm(prefix string, value any, values url.Values) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			field := key
			if prefix != "" {
				field = prefix + "[" + key + "]"
			}
			encodeStripeForm(field, typed[key], values)
		}
	case map[string]string:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			field := key
			if prefix != "" {
				field = prefix + "[" + key + "]"
			}
			values.Add(field, typed[key])
		}
	case []any:
		for _, item := range typed {
			encodeStripeForm(prefix+"[]", item, values)
		}
	case nil:
		return
	default:
		if prefix != "" {
			values.Add(prefix, fmt.Sprint(typed))
		}
	}
}

func objectMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"value": fmt.Sprint(value)}
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return map[string]any{"value": string(encoded)}
	}
	return decoded
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		copied := map[string]any{}
		for key, item := range value {
			copied[key] = item
		}
		return copied
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}
	return decoded
}

func resolveRequestTemplates(request map[string]any, observations map[string]observation) (map[string]any, error) {
	resolved, err := resolveTemplateValue(request, observations)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, nil
	}
	object, ok := resolved.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("resolved request must be an object")
	}
	return object, nil
}

func resolveTemplateValue(value any, observations map[string]observation) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		resolved := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			item, err := resolveTemplateValue(typed[key], observations)
			if err != nil {
				return nil, err
			}
			resolved[key] = item
		}
		return resolved, nil
	case []any:
		resolved := make([]any, len(typed))
		for idx, item := range typed {
			next, err := resolveTemplateValue(item, observations)
			if err != nil {
				return nil, err
			}
			resolved[idx] = next
		}
		return resolved, nil
	case string:
		return resolveTemplateString(typed, observations)
	default:
		return typed, nil
	}
}

func resolveTemplateString(value string, observations map[string]observation) (any, error) {
	start := strings.Index(value, "{{")
	if start < 0 {
		return value, nil
	}
	end := strings.Index(value[start+2:], "}}")
	if end < 0 {
		return nil, fmt.Errorf("template %q is missing closing braces", value)
	}
	end += start + 2
	if strings.TrimSpace(value[:start]) == "" && strings.TrimSpace(value[end+2:]) == "" {
		path := strings.TrimSpace(value[start+2 : end])
		resolved, err := resolveObservationPath(path, observations)
		if err != nil {
			return nil, err
		}
		return resolved, nil
	}

	var builder strings.Builder
	remainder := value
	for {
		start = strings.Index(remainder, "{{")
		if start < 0 {
			builder.WriteString(remainder)
			break
		}
		builder.WriteString(remainder[:start])
		end = strings.Index(remainder[start+2:], "}}")
		if end < 0 {
			return nil, fmt.Errorf("template %q is missing closing braces", value)
		}
		end += start + 2
		path := strings.TrimSpace(remainder[start+2 : end])
		resolved, err := resolveObservationPath(path, observations)
		if err != nil {
			return nil, err
		}
		builder.WriteString(stringValue(resolved))
		remainder = remainder[end+2:]
	}
	return builder.String(), nil
}

func resolveObservationPath(path string, observations map[string]observation) (any, error) {
	root := map[string]any{}
	for stepID, observed := range observations {
		root[stepID] = observationMap(observed)
	}
	value, ok := valueAtPath(root, path)
	if !ok {
		return nil, fmt.Errorf("could not resolve template path %q", path)
	}
	return value, nil
}

func statusCode(err error) int {
	if err == nil {
		return 200
	}
	var stripeErr *stripe.Error
	if errors.As(err, &stripeErr) {
		return stripeErr.StatusCode
	}
	return 500
}

func errorMap(err error) map[string]any {
	if err == nil {
		return nil
	}
	var stripeErr *stripe.Error
	if errors.As(err, &stripeErr) {
		return objectMap(stripeErr)
	}
	return map[string]any{"message": err.Error()}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		v, _ := typed.Int64()
		return v
	case string:
		v, _ := strconv.ParseInt(typed, 10, 64)
		return v
	default:
		return 0
	}
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}

func stringMap(value any) map[string]string {
	if value == nil {
		return nil
	}
	result := map[string]string{}
	switch typed := value.(type) {
	case map[string]string:
		for key, item := range typed {
			result[key] = item
		}
	case map[string]any:
		for key, item := range typed {
			result[key] = stringValue(item)
		}
	}
	return result
}

func optionalString(source map[string]any, key string) *string {
	if _, ok := source[key]; !ok {
		return nil
	}
	value := stringValue(source[key])
	return &value
}

func optionalInt64(source map[string]any, key string) *int64 {
	if _, ok := source[key]; !ok {
		return nil
	}
	value := int64Value(source[key])
	return &value
}

func billingDetails(value any) stripe.BillingDetails {
	object, _ := value.(map[string]any)
	return stripe.BillingDetails{
		Email: stringValue(object["email"]),
		Name:  stringValue(object["name"]),
		Phone: stringValue(object["phone"]),
	}
}

func optionalBillingDetails(source map[string]any, key string) *stripe.BillingDetails {
	if _, ok := source[key]; !ok {
		return nil
	}
	value := billingDetails(source[key])
	return &value
}

func card(value any) stripe.Card {
	object, _ := value.(map[string]any)
	return stripe.Card{
		Brand:    stringValue(object["brand"]),
		Last4:    stringValue(object["last4"]),
		ExpMonth: int(int64Value(object["exp_month"])),
		ExpYear:  int(int64Value(object["exp_year"])),
		Funding:  stringValue(object["funding"]),
	}
}

func simulatorPaymentMethodID(
	ctx context.Context,
	sim *stripe.Simulator,
	sessionName string,
	request map[string]any,
) (string, error) {
	paymentMethodData, ok := request["payment_method_data"].(map[string]any)
	if !ok || len(paymentMethodData) == 0 {
		return "", nil
	}
	cardData, _ := paymentMethodData["card"].(map[string]any)
	paymentMethod, err := sim.CreatePaymentMethod(ctx, sessionName, stripe.CreatePaymentMethodParams{
		Type:           firstNonEmptyString(stringValue(paymentMethodData["type"]), "card"),
		BillingDetails: billingDetails(paymentMethodData["billing_details"]),
		Card: stripe.Card{
			Brand:    firstNonEmptyString(stringValue(cardData["brand"]), "visa"),
			Last4:    firstNonEmptyString(stringValue(cardData["last4"]), "4242"),
			ExpMonth: int(int64OrDefault(cardData["exp_month"], 12)),
			ExpYear:  int(int64OrDefault(cardData["exp_year"], 2030)),
			Funding:  firstNonEmptyString(stringValue(cardData["funding"]), "credit"),
		},
		Metadata: stringMap(paymentMethodData["metadata"]),
	})
	if err != nil {
		return "", err
	}
	if customerID := strings.TrimSpace(stringValue(request["customer"])); customerID != "" {
		paymentMethod, err = sim.AttachPaymentMethod(ctx, sessionName, paymentMethod.ID, stripe.AttachPaymentMethodParams{
			CustomerID: customerID,
		})
		if err != nil {
			return "", err
		}
	}
	return paymentMethod.ID, nil
}

func int64OrDefault(value any, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	if parsed := int64Value(value); parsed != 0 {
		return parsed
	}
	return fallback
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func optionalCard(source map[string]any, key string) *stripe.Card {
	if _, ok := source[key]; !ok {
		return nil
	}
	value := card(source[key])
	return &value
}

func indexObservations(observations []observation, match MatchConfig) map[string]observation {
	indexed := map[string]observation{}
	counts := map[string]int{}
	for idx, observed := range observations {
		baseKey := observationBaseKey(observed, match, idx)
		counts[baseKey]++
		indexed[fmt.Sprintf("%s\x00%d", baseKey, counts[baseKey])] = observed
	}
	return indexed
}

func observationBaseKey(observed observation, match MatchConfig, idx int) string {
	switch match.Strategy {
	case MatchStrategyExactSequence:
		return fmt.Sprintf("%06d", idx)
	case MatchStrategyInteractionIdentity:
		parts := make([]string, 0, len(match.Keys))
		root := observationMap(observed)
		for _, key := range match.Keys {
			value, _ := valueAtPath(root, key)
			parts = append(parts, key+"="+canonical(value))
		}
		return strings.Join(parts, "|")
	case MatchStrategyOperationSequence:
		fallthrough
	default:
		return observed.Operation
	}
}

func observationMap(observed observation) map[string]any {
	return map[string]any{
		"step_id":        observed.StepID,
		"operation":      observed.Operation,
		"request":        observed.Request,
		"response":       observed.Response,
		"status_code":    observed.StatusCode,
		"error":          observed.Error,
		"interaction_id": observed.InteractionID,
	}
}

func sortedKeys(left map[string]observation, right map[string]observation) []string {
	seen := map[string]bool{}
	keys := make([]string, 0, len(left)+len(right))
	for key := range left {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range right {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func toleratedPaths(fields []ToleratedDiffField, operation string) map[string]bool {
	paths := map[string]bool{
		"interaction_id": true,
	}
	for _, field := range fields {
		if len(field.AppliesTo) > 0 && !contains(field.AppliesTo, operation) {
			continue
		}
		paths[strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(field.Path), "$."), ".")] = true
	}
	return paths
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func differingPaths(left any, right any) []string {
	paths := []string{}
	collectDiffs("", normalize(left), normalize(right), &paths)
	sort.Strings(paths)
	return paths
}

func collectDiffs(path string, left any, right any, paths *[]string) {
	if reflect.DeepEqual(left, right) {
		return
	}
	leftMap, leftMapOK := left.(map[string]any)
	rightMap, rightMapOK := right.(map[string]any)
	if leftMapOK && rightMapOK {
		keys := map[string]bool{}
		for key := range leftMap {
			keys[key] = true
		}
		for key := range rightMap {
			keys[key] = true
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			collectDiffs(joinPath(path, key), leftMap[key], rightMap[key], paths)
		}
		return
	}
	leftSlice, leftSliceOK := left.([]any)
	rightSlice, rightSliceOK := right.([]any)
	if leftSliceOK && rightSliceOK {
		maxLen := len(leftSlice)
		if len(rightSlice) > maxLen {
			maxLen = len(rightSlice)
		}
		for idx := 0; idx < maxLen; idx++ {
			var leftValue any
			var rightValue any
			if idx < len(leftSlice) {
				leftValue = leftSlice[idx]
			}
			if idx < len(rightSlice) {
				rightValue = rightSlice[idx]
			}
			collectDiffs(fmt.Sprintf("%s[%d]", path, idx), leftValue, rightValue, paths)
		}
		return
	}
	*paths = append(*paths, path)
}

func joinPath(base string, segment string) string {
	if base == "" {
		return segment
	}
	return base + "." + segment
}

func subtractPaths(paths []string, tolerated map[string]bool) []string {
	result := []string{}
	for _, path := range paths {
		if !pathTolerated(path, tolerated) {
			result = append(result, path)
		}
	}
	return result
}

func intersectPaths(paths []string, tolerated map[string]bool) []string {
	result := []string{}
	for _, path := range paths {
		if pathTolerated(path, tolerated) {
			result = append(result, path)
		}
	}
	return result
}

func removeInternalTolerances(paths []string) []string {
	filtered := paths[:0]
	for _, path := range paths {
		if path == "interaction_id" {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func pathTolerated(path string, tolerated map[string]bool) bool {
	if tolerated[path] {
		return true
	}
	for toleratedPath := range tolerated {
		if strings.HasPrefix(path, toleratedPath+".") || strings.HasPrefix(path, toleratedPath+"[") {
			return true
		}
	}
	return false
}

func valueAtPath(root any, path string) (any, bool) {
	current := normalize(root)
	for _, segment := range splitPath(path) {
		if segment.key == "" && segment.index == nil {
			return nil, false
		}
		if segment.key != "" {
			object, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			current, ok = object[segment.key]
			if !ok {
				return nil, false
			}
		}
		if segment.index != nil {
			items, ok := current.([]any)
			if !ok || *segment.index < 0 || *segment.index >= len(items) {
				return nil, false
			}
			current = items[*segment.index]
		}
	}
	return current, true
}

type pathSegment struct {
	key   string
	index *int
}

func splitPath(path string) []pathSegment {
	segments := []pathSegment{}
	for _, raw := range strings.Split(path, ".") {
		if raw == "" {
			segments = append(segments, pathSegment{})
			continue
		}
		remainder := raw
		for {
			open := strings.Index(remainder, "[")
			if open < 0 || !strings.HasSuffix(remainder, "]") {
				segments = append(segments, pathSegment{key: remainder})
				break
			}
			if open > 0 {
				segments = append(segments, pathSegment{key: remainder[:open]})
			}
			rawIndex := strings.TrimSuffix(remainder[open+1:], "]")
			index, err := strconv.Atoi(rawIndex)
			if err != nil {
				segments = append(segments, pathSegment{key: remainder})
				break
			}
			segments = append(segments, pathSegment{index: &index})
			break
		}
	}
	return segments
}

func canonical(value any) string {
	encoded, err := json.Marshal(normalize(value))
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

func normalize(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}
	return decoded
}
