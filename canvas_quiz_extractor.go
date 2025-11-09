package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type QuizChoice struct {
	ItemBody string `json:"item_body"`
	ID       string `json:"id"`
	Position int    `json:"position"`
}

type QuizBlank struct {
	AnswerType string `json:"answer_type"`
	ID         string `json:"id"`
}

type InteractionData struct {
	Blanks        []QuizBlank     `json:"blanks"`
	Choices       []QuizChoice    // normalized slice after unmarshal
	TrueChoice    string          `json:"true_choice"`
	FalseChoice   string          `json:"false_choice"`
	ShuffledOrder []string        `json:"shuffled_order"`
	RawChoices    json.RawMessage `json:"choices"` // holds raw map/array for secondary parse
}

type QuizItemInner struct {
	InteractionData  InteractionData `json:"interaction_data"`
	ItemBody         string          `json:"item_body"`
	UserResponseType string          `json:"user_response_type"`
	Title            string          `json:"title"`
	ID               string          `json:"id"`
	InteractionType  struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
		ID   string `json:"id"`
	} `json:"interaction_type"`
}

type QuizItem struct {
	CalculatorType string        `json:"calculator_type"`
	Item           QuizItemInner `json:"item"`
	PointsPossible float64       `json:"points_possible"`
	Position       int           `json:"position"`
	QuestionNumber int           `json:"question_number"`
}

type ResultValueEntry struct {
	ResultScore   *int   `json:"result_score,omitempty"`
	UserResponded *bool  `json:"user_responded,omitempty"`
	Correct       *bool  `json:"correct,omitempty"`
	UserResponse  string `json:"user_response,omitempty"`
	CorrectAnswer string `json:"correct_answer,omitempty"`
}

type ScoredData struct {
	Correct  bool            `json:"correct"`
	ValueRaw json.RawMessage `json:"value"`
}

type ResultItem struct {
	ItemID   string     `json:"item_id"`
	Position int        `json:"position"`
	Score    float64    `json:"score"`
	Scored   ScoredData `json:"scored_data"`
}

func mustReadJSON[T any](path string, v *T) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// stripHTML does a simple tag stripper and entity unescape for short HTML fragments.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	out := b.String()
	out = html.UnescapeString(out)
	out = strings.ReplaceAll(out, "\r", "")
	out = strings.ReplaceAll(out, "\n", " ")
	out = strings.TrimSpace(out)
	out = strings.Join(strings.Fields(out), " ")
	return out
}

// deriveCorrectChoiceIDs returns ids deemed correct from heterogeneous scored value structures.
func deriveCorrectChoiceIDs(res ResultItem) map[string]bool {
	ids := map[string]bool{}
	if len(res.Scored.ValueRaw) == 0 || string(res.Scored.ValueRaw) == "null" {
		return ids
	}
	// Try map form first
	var mapForm map[string]ResultValueEntry
	if err := json.Unmarshal(res.Scored.ValueRaw, &mapForm); err == nil && len(mapForm) > 0 {
		for id, entry := range mapForm {
			if entry.ResultScore != nil && *entry.ResultScore == 1 {
				ids[id] = true
			}
			if entry.Correct != nil && *entry.Correct {
				ids[id] = true
			}
		}
		return ids
	}
	// Try ordering / array form
	var arrayForm []struct {
		ID            any    `json:"id"`
		UserResponded string `json:"user_responded"`
		ResultScore   int    `json:"result_score"`
		Value         string `json:"value"`
	}
	if err := json.Unmarshal(res.Scored.ValueRaw, &arrayForm); err == nil {
		for _, row := range arrayForm {
			if row.ResultScore == 1 {
				// For ordering questions, value is the correct choice id.
				if row.Value != "" {
					ids[row.Value] = true
				}
			}
		}
	}
	return ids
}

// normalizeChoices ensures InteractionData.Choices is populated from various Canvas encodings.
func (idat *InteractionData) normalizeChoices(userRespType, interactionSlug string) {
	if len(idat.Choices) > 0 { // already standard array
		return
	}
	// Boolean true/false
	if strings.EqualFold(userRespType, "Boolean") || interactionSlug == "true-false" {
		trueLabel := idat.TrueChoice
		falseLabel := idat.FalseChoice
		if trueLabel == "" {
			trueLabel = "True"
		}
		if falseLabel == "" {
			falseLabel = "False"
		}
		idat.Choices = []QuizChoice{{ItemBody: trueLabel, ID: "true", Position: 1}, {ItemBody: falseLabel, ID: "false", Position: 2}}
		return
	}
	if len(idat.RawChoices) == 0 {
		return
	}
	// Attempt map form
	var mapChoices map[string]struct {
		ItemBody string `json:"item_body"`
		ID       string `json:"id"`
	}
	if err := json.Unmarshal(idat.RawChoices, &mapChoices); err == nil && len(mapChoices) > 0 {
		order := idat.ShuffledOrder
		pos := 1
		if len(order) > 0 {
			for _, cid := range order {
				if mc, ok := mapChoices[cid]; ok {
					label := mc.ItemBody
					if label == "" {
						label = mapChoices[cid].ItemBody
					}
					idat.Choices = append(idat.Choices, QuizChoice{ItemBody: label, ID: cid, Position: pos})
					pos++
				}
			}
		}
		// Fallback add remaining not in order
		if len(idat.Choices) == 0 {
			for _, mc := range mapChoices {
				idVal := mc.ID
				if idVal == "" {
					// use key? we don't have key variable here; skip
					continue
				}
				idat.Choices = append(idat.Choices, QuizChoice{ItemBody: mc.ItemBody, ID: idVal, Position: pos})
				pos++
			}
		}
		return
	}
	// Attempt array form (already attempted earlier but ensure we decode if RawChoices contains array shape differing from struct tag)
	var arr []QuizChoice
	if err := json.Unmarshal(idat.RawChoices, &arr); err == nil && len(arr) > 0 {
		idat.Choices = arr
	}
}

func findResultByID(results []ResultItem, id string) (ResultItem, error) {
	for _, r := range results {
		if r.ItemID == id {
			return r, nil
		}
	}
	return ResultItem{}, errors.New("result not found for item_id=" + id)
}

func writeMarkdown(outPath string, quiz []QuizItem, results []ResultItem, weekLabel string) error {
	var sb strings.Builder
	// Derive a nicer week-specific header if possible (e.g. wk03 -> WK03)
	cleanWeek := strings.TrimSpace(weekLabel)
	if cleanWeek == "" {
		// attempt fallback: read from output filename
		base := filepath.Base(outPath)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		// match wk followed by digits
		re := regexp.MustCompile(`(?i)^(wk\d{2})`)
		if m := re.FindStringSubmatch(name); len(m) > 1 {
			cleanWeek = strings.ToUpper(m[1])
		}
	}
	if cleanWeek != "" {
		sb.WriteString(fmt.Sprintf("# %s Quiz — Questions and Solutions\n\n", strings.ToUpper(cleanWeek)))
	} else {
		sb.WriteString("# WK Quiz — Questions and Solutions\n\n")
	}

	sorted := make([]QuizItem, len(quiz))
	copy(sorted, quiz)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Position != sorted[j].Position {
			return sorted[i].Position < sorted[j].Position
		}
		if sorted[i].QuestionNumber != sorted[j].QuestionNumber {
			return sorted[i].QuestionNumber < sorted[j].QuestionNumber
		}
		return i < j
	})

	for idx, q := range sorted {
		questionText := stripHTML(q.Item.ItemBody)
		num := idx + 1
		sb.WriteString(fmt.Sprintf("## %d) %s\n", num, questionText))

		res, err := findResultByID(results, q.Item.ID)
		if err != nil {
			sb.WriteString("- Options: (no result data)\n\n")
			continue
		}

		isBlank := len(q.Item.InteractionData.Blanks) > 0
		// Normalize choices given heterogeneous encodings
		q.Item.InteractionData.normalizeChoices(q.Item.UserResponseType, q.Item.InteractionType.Slug)
		choices := q.Item.InteractionData.Choices

		if isBlank {
			sb.WriteString("- Options: N/A (open entry)\n\n")
			ans := ""
			// Extract answer from scored data map form if present
			if len(q.Item.InteractionData.Blanks) > 0 && len(res.Scored.ValueRaw) > 0 {
				var mapForm map[string]ResultValueEntry
				if err2 := json.Unmarshal(res.Scored.ValueRaw, &mapForm); err2 == nil {
					bid := q.Item.InteractionData.Blanks[0].ID
					if v, ok := mapForm[bid]; ok {
						if v.CorrectAnswer != "" {
							ans = v.CorrectAnswer
						} else if v.UserResponse != "" {
							ans = v.UserResponse
						}
					}
				}
			}
			if ans == "" {
				ans = "(answer unavailable)"
			}
			sb.WriteString(fmt.Sprintf("- Answer: %s\n\n", stripHTML(ans)))
			continue
		}

		correctIDs := deriveCorrectChoiceIDs(res)
		if len(choices) > 0 {
			sb.WriteString("- Options:\n")
			sort.SliceStable(choices, func(i, j int) bool { return choices[i].Position < choices[j].Position })
			for _, c := range choices {
				label := stripHTML(c.ItemBody)
				if correctIDs[c.ID] {
					sb.WriteString(fmt.Sprintf("  - %s (correct)\n", label))
				} else {
					sb.WriteString(fmt.Sprintf("  - %s\n", label))
				}
			}
			sb.WriteString("\n")
		}

		var correctLabels []string
		for _, c := range choices {
			if correctIDs[c.ID] {
				correctLabels = append(correctLabels, stripHTML(c.ItemBody))
			}
		}

		if strings.Contains(strings.ToLower(q.Item.UserResponseType), "multipleuuid") || len(correctLabels) > 1 {
			sb.WriteString("- Correct answers:\n")
			for _, l := range correctLabels {
				sb.WriteString(fmt.Sprintf("  - %s\n", l))
			}
			sb.WriteString("\n")
		} else if len(correctLabels) == 1 {
			sb.WriteString(fmt.Sprintf("- Answer: %s\n\n", correctLabels[0]))
		} else {
			sb.WriteString("- Answer: (answer unavailable)\n\n")
		}
	}

	if err := os.WriteFile(outPath, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	return nil
}

func main() {
	var (
		quizPath   string
		resultPath string
		outPath    string
	)
	flag.StringVar(&quizPath, "in", "", "Path to quiz JSON (e.g., wk12.json). If empty, you'll be prompted.")
	flag.StringVar(&resultPath, "results", "", "Path to results JSON (e.g., wk12_result.json). If empty, you'll be prompted.")
	flag.StringVar(&outPath, "out", "", "Output Markdown file path. If empty, derived from the first 4 chars of quiz filename.")
	flag.Parse()

	reader := bufio.NewReader(os.Stdin)
	if strings.TrimSpace(quizPath) == "" {
		fmt.Print("Enter quiz JSON path (e.g., wk12.json): ")
		line, _ := reader.ReadString('\n')
		quizPath = strings.TrimSpace(line)
	}
	if strings.TrimSpace(resultPath) == "" {
		fmt.Print("Enter results JSON path (e.g., wk12_result.json): ")
		line, _ := reader.ReadString('\n')
		resultPath = strings.TrimSpace(line)
	}

	if strings.TrimSpace(outPath) == "" {
		base := filepath.Base(quizPath)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		r := []rune(name)
		prefix := name
		if len(r) >= 4 {
			prefix = string(r[:4])
		}
		outPath = filepath.Join(filepath.Dir(quizPath), fmt.Sprintf("%s_quiz_solutions.md", prefix))
	}

	qp, _ := filepath.Abs(quizPath)
	rp, _ := filepath.Abs(resultPath)
	op, _ := filepath.Abs(outPath)

	var quiz []QuizItem
	if err := mustReadJSON(qp, &quiz); err != nil {
		fmt.Fprintf(os.Stderr, "failed to read quiz JSON %s: %v\n", qp, err)
		os.Exit(1)
	}
	var results []ResultItem
	if err := mustReadJSON(rp, &results); err != nil {
		fmt.Fprintf(os.Stderr, "failed to read result JSON %s: %v\n", rp, err)
		os.Exit(1)
	}

	// Derive week label from quiz filename (e.g., wk12.json -> WK12)
	weekLabel := ""
	{
		base := filepath.Base(qp)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		re := regexp.MustCompile(`(?i)^(wk\d{2})`)
		if m := re.FindStringSubmatch(name); len(m) > 1 {
			weekLabel = strings.ToUpper(m[1])
		}
	}

	if err := writeMarkdown(op, quiz, results, weekLabel); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write markdown %s: %v\n", op, err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s from %s and %s\n", op, qp, rp)
}
