package models

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// QuestionsSchemaVersion is sent alongside exam.questions so clients can
// distinguish the canonical v1 shape from pre-contract/legacy payloads.
const QuestionsSchemaVersion = 1

// CanonicalizeQuestions converts a legacy questions payload into the v1
// contract shape. It is intentionally lossless for scoring-relevant data:
//
//   - numbers become integers and invalid/missing numbers use the same
//     positional fallback as Android's AnswerSheetPlanner;
//   - both `options` and `choices` are emitted with identical content so old
//     Go consumers and Android keep working during the compatibility window;
//   - only the documented fields survive (number, type, weight,
//     partial_scoring, label, choices/options, statements,
//     left_items/right_items, numbering, key). Legacy `answer` becomes `key`.
//
// It operates in memory: existing database rows are never rewritten.
func CanonicalizeQuestions(questions []map[string]interface{}) []map[string]interface{} {
	if len(questions) == 0 {
		return []map[string]interface{}{}
	}

	rawNumbers := make([]interface{}, len(questions))
	for i, q := range questions {
		if q != nil {
			rawNumbers[i] = q["number"]
		}
	}
	numbers := questionNumbersFromRaw(rawNumbers)

	out := make([]map[string]interface{}, len(questions))
	for i, q := range questions {
		canonical := map[string]interface{}{
			"number":          numbers[i],
			"type":            normalizeQuestionType(questionTypeValue(q)),
			"weight":          normalizeQuestionWeight(q["weight"]),
			"partial_scoring": normalizeQuestionBool(q["partial_scoring"]),
		}

		if q == nil {
			out[i] = canonical
			continue
		}
		if label := strings.TrimSpace(questionString(q["label"])); label != "" {
			canonical["label"] = label
		}

		switch canonical["type"] {
		case "single_choice", "multiple_choice":
			// `options` is the legacy save/validation authority; `choices` is
			// the canonical name read by Android. Both are mirrored below.
			choices := questionStringList(q["options"])
			if len(choices) == 0 {
				choices = questionStringList(q["choices"])
			}
			if len(choices) == 0 {
				for _, alias := range []string{"pilihan", "pilihan_jawaban", "items"} {
					if choices = questionStringList(q[alias]); len(choices) > 0 {
						break
					}
				}
			}
			if len(choices) > 0 {
				values := toInterfaceSlice(choices)
				canonical["choices"] = values
				// Compatibility mirror for older Go/desktop clients.
				canonical["options"] = values
			}
		case "true_false":
			if statements := questionStringList(q["statements"]); len(statements) > 0 {
				trimmed := make([]string, len(statements))
				for i, s := range statements {
					trimmed[i] = strings.TrimSpace(s)
				}
				canonical["statements"] = toInterfaceSlice(trimmed)
			}
		case "matching":
			if left := questionStringList(q["left_items"]); len(left) > 0 {
				canonical["left_items"] = toInterfaceSlice(left)
			}
			if right := questionStringList(q["right_items"]); len(right) > 0 {
				canonical["right_items"] = toInterfaceSlice(right)
			}
			// Penomoran matching: "single" (default lama, satu nomor untuk
			// seluruh kartu) atau "range" (tiap item kiri memakai nomor
			// sendiri, sehingga kartu berikutnya mulai setelah rentang).
			numbering := "single"
			if strings.EqualFold(strings.TrimSpace(questionString(q["numbering"])), "range") {
				numbering = "range"
			}
			canonical["numbering"] = numbering
		}

		key, hasKey := q["key"]
		if !hasKey || key == nil {
			key = q["answer"]
		}
		if key != nil {
			canonical["key"] = canonicalQuestionKey(canonical["type"].(string), key)
		}

		out[i] = canonical
	}
	return out
}

// CanonicalizeQuestionsValue accepts the generic shape produced by
// json.Unmarshal ([]interface{} of map[string]interface{}) and returns the
// canonical payload when the input looks like a questions array.
func CanonicalizeQuestionsValue(raw interface{}) ([]map[string]interface{}, bool) {
	switch v := raw.(type) {
	case []map[string]interface{}:
		return CanonicalizeQuestions(v), true
	case []interface{}:
		questions := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			q, ok := item.(map[string]interface{})
			if !ok {
				return nil, false
			}
			questions = append(questions, q)
		}
		return CanonicalizeQuestions(questions), true
	default:
		return nil, false
	}
}

func questionTypeValue(q map[string]interface{}) string {
	if q == nil {
		return ""
	}
	return questionString(q["type"])
}

func questionString(v interface{}) string {
	switch value := v.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	case fmt.Stringer:
		return value.String()
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case int32:
		return strconv.FormatInt(int64(value), 10)
	case bool:
		return strconv.FormatBool(value)
	default:
		return ""
	}
}

// questionStringList accepts []interface{} (json.Unmarshal), []string, and
// legacy object entries ({text/label/value/...}) used by older admin builds.
func questionStringList(v interface{}) []string {
	var raw []interface{}
	switch list := v.(type) {
	case []interface{}:
		raw = list
	case []string:
		raw = make([]interface{}, len(list))
		for i, s := range list {
			raw[i] = s
		}
	default:
		return nil
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		var text string
		switch entry := item.(type) {
		case map[string]interface{}:
			for _, key := range []string{"text", "label", "value", "content", "name", "option"} {
				if candidate := strings.TrimSpace(questionString(entry[key])); candidate != "" {
					text = candidate
					break
				}
			}
		default:
			text = strings.TrimSpace(questionString(entry))
		}
		if text != "" {
			out = append(out, text)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toInterfaceSlice(values []string) []interface{} {
	out := make([]interface{}, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func normalizeQuestionWeight(raw interface{}) float64 {
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			return v
		}
	case float32:
		if v > 0 {
			return float64(v)
		}
	case int:
		if v > 0 {
			return float64(v)
		}
	case int64:
		if v > 0 {
			return float64(v)
		}
	case int32:
		if v > 0 {
			return float64(v)
		}
	case json.Number:
		if f, err := v.Float64(); err == nil && f > 0 {
			return f
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f > 0 {
			return f
		}
	}
	return 1.0
}

func normalizeQuestionBool(raw interface{}) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "ya":
			return true
		}
	case float64:
		return v != 0
	case int:
		return v != 0
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n != 0
		}
	}
	return false
}

// canonicalQuestionKey normalizes only the shapes the scoring engine compares
// case-insensitively: true/false keys are upper-cased, list keys are trimmed.
// Matching maps and short-answer strings keep their content.
func canonicalQuestionKey(qType string, key interface{}) interface{} {
	switch qType {
	case "true_false":
		if list, ok := toStringList(key); ok {
			out := make([]interface{}, len(list))
			for i, item := range list {
				out[i] = strings.ToUpper(strings.TrimSpace(item))
			}
			return out
		}
		return strings.ToUpper(strings.TrimSpace(questionString(key)))
	case "multiple_choice":
		if list, ok := toStringList(key); ok {
			out := make([]interface{}, len(list))
			for i, item := range list {
				out[i] = strings.TrimSpace(item)
			}
			return out
		}
	}
	return key
}
