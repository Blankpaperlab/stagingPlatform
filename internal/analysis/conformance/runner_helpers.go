package conformance

import (
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
	case stripe.OperationPaymentIntentsRetrieve:
		return http.MethodGet, base + "/payment_intents/" + id, nil
	case stripe.OperationPaymentIntentsUpdate:
		return http.MethodPost, base + "/payment_intents/" + id, nil
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
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			return nil, false
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
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
