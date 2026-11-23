package prompt

import (
	"strings"
)

// ForSuggest asks for a parseable COMMAND + optional one-line NOTE.
// Kept tiny so the Cursor ask round-trip stays fast.
func ForSuggest(ctxBlock string) string {
	var sb strings.Builder
	sb.WriteString(`You are hey, a fast terminal assistant. Reply with a shell command for the user.

Output EXACTLY this format (no markdown, no fences, no extra lines before COMMAND):
COMMAND: <single shell command>
NOTE: <optional one short sentence>

Rules:
- COMMAND is required; one line; no leading $. Prefer a safe, common invocation.
- NOTE is optional; at most one line; skip NOTE if nothing useful to say.
- If multiple steps are needed, join with && or one pipeline.
- Do not use tools, search a codebase, or ask questions — answer from the question and context alone.
- If you cannot suggest a command, reply with:
COMMAND: # hey: cannot suggest a command
NOTE: Need a more specific question.

`)
	sb.WriteString(ctxBlock)
	return sb.String()
}

// ForFill is kept as an alias of ForSuggest for callers/tests.
func ForFill(ctxBlock string) string { return ForSuggest(ctxBlock) }

// ForExplain asks for the same parseable shape (human printers style the note).
func ForExplain(ctxBlock string) string { return ForSuggest(ctxBlock) }

// Suggestion is a parsed model reply.
type Suggestion struct {
	Command string
	Note    string
}

// ParseSuggestion extracts COMMAND and optional NOTE from model output.
func ParseSuggestion(raw string) Suggestion {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Suggestion{}
	}

	// Strip markdown fences if the model ignored instructions.
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		var body []string
		for i, line := range lines {
			if i == 0 {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				break
			}
			body = append(body, line)
		}
		raw = strings.TrimSpace(strings.Join(body, "\n"))
	}

	var sug Suggestion
	var leftover []string
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		upper := strings.ToUpper(trim)
		switch {
		case strings.HasPrefix(upper, "COMMAND:"):
			sug.Command = cleanCommand(trim[len("COMMAND:"):])
		case strings.HasPrefix(upper, "CMD:"):
			sug.Command = cleanCommand(trim[len("CMD:"):])
		case strings.HasPrefix(upper, "NOTE:"):
			sug.Note = strings.TrimSpace(trim[len("NOTE:"):])
		default:
			leftover = append(leftover, trim)
		}
	}

	if sug.Command == "" && len(leftover) > 0 {
		// Fallback: first non-empty line is the command; rest may be a note.
		sug.Command = cleanCommand(leftover[0])
		if sug.Note == "" && len(leftover) > 1 {
			sug.Note = strings.TrimSpace(strings.Join(leftover[1:], " "))
		}
	}
	return sug
}

func cleanCommand(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$ ")
	return strings.TrimSpace(s)
}

// ExtractCommand pulls the command from model output (fill helpers / tests).
func ExtractCommand(raw string) string {
	return ParseSuggestion(raw).Command
}

// EncodeCapture is the machine-readable stdout protocol shell hooks parse.
func EncodeCapture(sug Suggestion) string {
	var sb strings.Builder
	sb.WriteString("COMMAND:")
	sb.WriteString(sug.Command)
	sb.WriteByte('\n')
	if sug.Note != "" {
		sb.WriteString("NOTE:")
		sb.WriteString(sug.Note)
		sb.WriteByte('\n')
	}
	return sb.String()
}
