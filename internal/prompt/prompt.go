package prompt

import "strings"

// ForFill asks the model to return only a shell command (for prompt insertion).
func ForFill(ctxBlock string) string {
	var sb strings.Builder
	sb.WriteString(`You are hey, a terminal assistant. Suggest a single shell command the user can run.

Rules:
- Reply with ONLY the shell command — no markdown fences, no explanation, no quotes around the whole command.
- Prefer a command that solves the question given recent history and cwd.
- If multiple steps are required, join with && or provide one pipeline.
- Do not invent destructive flags the user did not imply; prefer safe, common invocations.
- If the question cannot be answered with a shell command, reply with exactly: # hey: cannot suggest a command

`)
	sb.WriteString(ctxBlock)
	return sb.String()
}

// ForExplain asks for a short explanation plus the command (print mode).
func ForExplain(ctxBlock string) string {
	var sb strings.Builder
	sb.WriteString(`You are hey, a terminal assistant. The user asked a shell/how-to question.

Rules:
- Prefer answering with a shell command when the question is "how do I …" style.
- First line: the shell command only (no markdown fences).
- Then a blank line, then at most two short sentences of explanation.
- If no command applies, explain briefly without a fake command.

`)
	sb.WriteString(ctxBlock)
	return sb.String()
}

// ExtractCommand pulls the first non-empty line from model output for fill mode.
func ExtractCommand(raw string) string {
	raw = strings.TrimSpace(raw)
	// Strip common markdown fences if the model ignored instructions.
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
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "$ ")
		return line
	}
	return ""
}
