// Package helpers — scoring utilities ported from server/helpers.py.
package helpers

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Question represents a single question entry from the questions_json array.
type Question struct {
	Number         interface{} `json:"number"`
	Key            interface{} `json:"key"`
	Answer         interface{} `json:"answer"`
	Type           string      `json:"type"`
	Weight         float64     `json:"weight"`
	Score          float64     `json:"score"`
	PartialScoring bool        `json:"partial_scoring"`
	Label          string      `json:"label,omitempty"`
}

// GetAnswerKey returns the correct answer, checking both Key and Answer fields.
func (q *Question) GetAnswerKey() interface{} {
	if q.Key != nil {
		return q.Key
	}
	return q.Answer
}

// GetWeight returns the question weight, checking both Weight and Score fields.
func (q *Question) GetWeight() float64 {
	if q.Weight > 0 {
		return q.Weight
	}
	if q.Score > 0 {
		return q.Score
	}
	return 1.0
}

// Evaluation holds the result of evaluating a single question.
type Evaluation struct {
	Earned      float64 `json:"earned"`
	StatusText  string  `json:"statusText"`
	StatusClass string  `json:"statusClass"`
}

const (
	StatusCorrect    = "correct"
	StatusIncorrect  = "incorrect"
	StatusPartial    = "partial"
	StatusUnanswered = "unanswered"
)

// NormalizeQNum normalizes a question number, handling float representations
// such as 1.0 -> "1". Ported from helpers.py _normalize_q_num.
func NormalizeQNum(number interface{}) string {
	switch v := number.(type) {
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			if f == float64(int64(f)) {
				return strconv.FormatInt(int64(f), 10)
			}
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// EvaluateSingleQuestion evaluates a single student answer against the correct
// answer. Returns (earned, statusText, statusClass).
// Ported from helpers.py _evaluate_single_question.
func EvaluateSingleQuestion(studentAns, correctAns interface{}, qType string, qWeight float64, partialScoring bool) (float64, string, string) {
	if studentAns == nil || correctAns == nil {
		if studentAns == nil {
			return 0, StatusUnanswered, StatusUnanswered
		}
		return 0, StatusIncorrect, StatusIncorrect
	}

	switch qType {
	case "single_choice", "true_false", "short_answer":
		sNorm := strings.ToUpper(strings.Join(strings.Fields(fmt.Sprintf("%v", studentAns)), " "))
		cNorm := strings.ToUpper(strings.Join(strings.Fields(fmt.Sprintf("%v", correctAns)), " "))
		if sNorm == cNorm {
			return qWeight, StatusCorrect, StatusCorrect
		}
		return 0, StatusIncorrect, StatusIncorrect

	case "multiple_choice":
		studentList, sOK := studentAns.([]interface{})
		correctList, cOK := correctAns.([]interface{})
		if !sOK || !cOK {
			sStr := strings.Fields(fmt.Sprintf("%v", studentAns))
			cStr := strings.Fields(fmt.Sprintf("%v", correctAns))
			return evaluateMC(sStr, cStr, qWeight, partialScoring)
		}
		return evaluateMC(stripSlice(studentList), stripSlice(correctList), qWeight, partialScoring)

	case "matching":
		studentMap, sOK := studentAns.(map[string]interface{})
		correctMap, cOK := correctAns.(map[string]interface{})
		if !sOK || !cOK {
			sMap := toMapStringInterface(studentAns)
			cMap := toMapStringInterface(correctAns)
			if sMap == nil || cMap == nil {
				return 0, StatusIncorrect, StatusIncorrect
			}
			return evaluateMatching(sMap, cMap, qWeight, partialScoring)
		}
		return evaluateMatching(studentMap, correctMap, qWeight, partialScoring)
	}

	return 0, StatusIncorrect, StatusIncorrect
}

// EvaluateAnswersDetailed evaluates all student answers against the question
// set and returns a map keyed by question number.
// Ported from helpers.py evaluate_answers_detailed.
func EvaluateAnswersDetailed(answers map[string]interface{}, questions []Question) map[string]Evaluation {
	result := make(map[string]Evaluation)
	if len(questions) == 0 {
		return result
	}
	for _, q := range questions {
		qNum := NormalizeQNum(q.Number)
		studentAns := answers[qNum]
		correctAns := q.GetAnswerKey()
		qWeight := q.GetWeight()
		earned, statusText, statusClass := EvaluateSingleQuestion(studentAns, correctAns, q.Type, qWeight, q.PartialScoring)
		result[qNum] = Evaluation{
			Earned:      earned,
			StatusText:  statusText,
			StatusClass: statusClass,
		}
	}
	return result
}

// CalculateSubmissionScore computes the total score for a submission.
// Returns 0 if questions are empty, matching the Python default.
// Ported from helpers.py calculate_submission_score.
func CalculateSubmissionScore(answers map[string]interface{}, questions []Question) float64 {
	if len(questions) == 0 {
		return 0
	}
	evaluation := EvaluateAnswersDetailed(answers, questions)
	var total float64
	for _, ev := range evaluation {
		total += ev.Earned
	}
	return math.Round(total*100) / 100
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func stripSlice(items []interface{}) []string {
	result := make([]string, len(items))
	for i, v := range items {
		result[i] = strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", v)))
	}
	return result
}

func toMapStringInterface(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func evaluateMC(studentSet, correctSet []string, qWeight float64, partialScoring bool) (float64, string, string) {
	if partialScoring {
		if len(correctSet) == 0 {
			return 0, StatusIncorrect, StatusIncorrect
		}
		correctMap := make(map[string]bool, len(correctSet))
		for _, c := range correctSet {
			correctMap[c] = true
		}

		correctSelected := 0
		incorrectSelected := 0
		for _, s := range studentSet {
			if correctMap[s] {
				correctSelected++
			} else {
				incorrectSelected++
			}
		}

		portion := math.Max(0, float64(correctSelected-incorrectSelected))/float64(len(correctSet))
		earned := portion * qWeight

		switch {
		case portion >= 1.0:
			return earned, StatusCorrect, StatusCorrect
		case portion > 0:
			return earned, StatusPartial, StatusPartial
		default:
			return 0, StatusIncorrect, StatusIncorrect
		}
	}

	// Exact match scoring.
	if len(studentSet) != len(correctSet) {
		return 0, StatusIncorrect, StatusIncorrect
	}
	sCopy := make([]string, len(studentSet))
	cCopy := make([]string, len(correctSet))
	copy(sCopy, studentSet)
	copy(cCopy, correctSet)
	bubbleSort(sCopy)
	bubbleSort(cCopy)
	for i := range sCopy {
		if sCopy[i] != cCopy[i] {
			return 0, StatusIncorrect, StatusIncorrect
		}
	}
	return qWeight, StatusCorrect, StatusCorrect
}

func evaluateMatching(studentMap, correctMap map[string]interface{}, qWeight float64, partialScoring bool) (float64, string, string) {
	if partialScoring {
		if len(correctMap) == 0 {
			return 0, StatusIncorrect, StatusIncorrect
		}
		correctMatches := 0
		for k, v := range correctMap {
			sv, ok := studentMap[k]
			if !ok {
				continue
			}
			sNorm := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", sv)))
			cNorm := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", v)))
			if sNorm == cNorm {
				correctMatches++
			}
		}
		portion := float64(correctMatches) / float64(len(correctMap))
		earned := portion * qWeight
		switch {
		case portion >= 1.0:
			return earned, StatusCorrect, StatusCorrect
		case portion > 0:
			return earned, StatusPartial, StatusPartial
		default:
			return 0, StatusIncorrect, StatusIncorrect
		}
	}

	// Exact match: all key-value pairs must match.
	for k, v := range correctMap {
		sv, ok := studentMap[k]
		if !ok {
			return 0, StatusIncorrect, StatusIncorrect
		}
		sNorm := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", sv)))
		cNorm := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", v)))
		if sNorm != cNorm {
			return 0, StatusIncorrect, StatusIncorrect
		}
	}
	return qWeight, StatusCorrect, StatusCorrect
}

func bubbleSort(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
