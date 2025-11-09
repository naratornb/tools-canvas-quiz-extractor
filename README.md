# tools-canvas-quiz-extractor
From the pain of recollection and extraction of quiz from canvas platform. I create a quick solution using golang to extract json formatted quiz and solution set to prepare a human readable (text) format for final exam prep.


This tool generates a human-readable Markdown file of quiz questions and solutions by combining a quiz definition JSON (e.g., `wk12.json`) with its graded results JSON (e.g., `wk12_result.json`). It mirrors the format you saw in `wk12_quiz_solutions.md`, including all options and marking correct choices.

## What it does

- Parses the quiz JSON to extract:
  - Question text (with simple HTML stripped)
  - Choices (for multiple choice/multi-answer) or blanks (for fill-in-the-blank)
- Parses the results JSON to determine correctness:
  - Correct choices are inferred when `result_score == 1` or `correct == true`
  - For fill-in-the-blank, uses `correct_answer` or falls back to `user_response`
- Outputs a Markdown file with:
  - Options under each question (correct ones annotated with `(correct)`)
  - A blank line after options for readability
  - Either a single `Answer:` or a `Correct answers:` list

## File

- `canvas_quiz_extractor.go` — the Go program that produces the Markdown.

## Prerequisites

- Go 1.20+ installed
- The input files in the same folder (or provide absolute/relative paths):
  - Quiz: `wkNN.json` (e.g., `wk12.json`)
  - Results: `wkNN_result.json` (e.g., `wk12_result.json`)

## Usage

You can run the script non-interactively (flags) or interactively (prompts when flags are omitted).

### Flags

- `-in` (string): Path to quiz JSON (e.g., `wk12.json`). If omitted, you'll be prompted.
- `-results` (string): Path to results JSON (e.g., `wk12_result.json`). If omitted, you'll be prompted.
- `-out` (string): Output Markdown path. If omitted, it's derived from the first 4 characters of the quiz filename.

### Dynamic output naming

When `-out` is not provided, the program derives the output filename as:

- Take the quiz filename (basename without extension), e.g., `wk12` from `wk12.json`.
- Truncate to the first 4 characters (e.g., `wk12`, `wk01a` → `wk01`).
- Write `<prefix>_quiz_solutions.md` in the same directory as the quiz file.

Examples:
- `wk01.json` → `wk01_quiz_solutions.md`
- `wk42_extra.json` → `wk42_quiz_solutions.md`

### Examples

Non-interactive (full control):

```bash
# In the Quiz directory
go run canvas_quiz_extractor.go -in wk12.json -results wk12_result.json -out wk12_quiz_solutions.md
```

Let the program derive the output name:

```bash
go run canvas_quiz_extractor.go -in wk07.json -results wk07_result.json
# → writes wk07_quiz_solutions.md next to the quiz file
```

Interactive (no flags):

```bash
go run canvas_quiz_extractor.go
# Prompts for quiz JSON and results JSON, then derives output name.
```

## Input format assumptions

- Quiz JSON structure (simplified):
  - `item.item_body` contains HTML for the question stem
  - `item.interaction_data.choices[]` contains choices with `item_body`, `id`, `position`
  - `item.interaction_data.blanks[]` contains blanks for fill-in-the-blank
  - `id` at the top level of each item is the `item_id` used by the results
- Results JSON structure (simplified):
  - Each object corresponds to a graded item and has `item_id`
  - `scored_data.value` is a map keyed by choice/blank IDs
  - Each value may include `result_score` (1 means correct), `correct`, `user_response`, `correct_answer`

## Output format

The Markdown groups each question as:

```
## N) <Question text>
- Options:
  - <Choice A>
  - <Choice B> (correct)

- Answer: <Single correct>
# or for multi-answer
- Correct answers:
  - <Correct A>
  - <Correct B>
```

Fill-in-the-blank questions are shown as:

```
## N) <Question with ____>
- Options: N/A (open entry)

- Answer: <text>
```

## Implementation notes

- HTML stripping: A simple tag dropper removes `<...>` tags and unescapes entities.
- Ordering: Questions are sorted by `position`, then `question_number`; choices by `position`.
- Robustness: If a result entry isn't found for an item, the question is still emitted with a placeholder.
- Multi-answer detection: If multiple choices are marked correct (or type is `MultipleUuid`), the output uses a `Correct answers:` list.

## Troubleshooting

- If you see `(answer unavailable)`, the expected fields weren't present in results.
- Ensure the `item_id` in the quiz matches the `item_id` in results.
- If your quiz filenames differ from `wkNN.json`, that's fine — the tool only uses the first 4 characters of the base filename when deriving the output name.

## License

Internal/educational use. Update this section if you plan to share or open source.
