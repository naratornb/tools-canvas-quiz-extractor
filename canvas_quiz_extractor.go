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
	Blanks  []QuizBlank  `json:"blanks"`
	Choices []QuizChoice `json:"choices"`
}

type QuizItemInner struct {
	InteractionData  InteractionData `json:"interaction_data"`
	ItemBody         string          `json:"item_body"`
	UserResponseType string          `json:"user_response_type"`
	Title            string          `json:"title"`
	ID               string          `json:"id"`
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
	Correct bool                        `json:"correct"`
	Value   map[string]ResultValueEntry `json:"value"`
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

func deriveCorrectChoiceIDs(res ResultItem) map[string]bool {
	ids := map[string]bool{}
	for id, entry := range res.Scored.Value {
		if entry.ResultScore != nil && *entry.ResultScore == 1 {
			ids[id] = true
		}
		if entry.Correct != nil && *entry.Correct {
			ids[id] = true
		}
	}
	return ids
}

func findResultByID(results []ResultItem, id string) (ResultItem, error) {
	for _, r := range results {
		if r.ItemID == id {
			return r, nil
		}
	}
	return ResultItem{}, errors.New("result not found for item_id=" + id)
}

func writeMarkdown(outPath string, quiz []QuizItem, results []ResultItem) error {
	var sb strings.Builder
	sb.WriteString("# WK Quiz — Questions and Solutions\n\n")

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
		choices := q.Item.InteractionData.Choices

		if isBlank {
			sb.WriteString("- Options: N/A (open entry)\n\n")
			ans := ""
			if len(q.Item.InteractionData.Blanks) > 0 {
				bid := q.Item.InteractionData.Blanks[0].ID
				if v, ok := res.Scored.Value[bid]; ok {
					if v.CorrectAnswer != "" {
						ans = v.CorrectAnswer
					} else if v.UserResponse != "" {
						ans = v.UserResponse
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

	if err := writeMarkdown(op, quiz, results); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write markdown %s: %v\n", op, err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s from %s and %s\n", op, qp, rp)
}
