package whatsapp

import (
	"fmt"
	"regexp"
	"strings"
)

// markdownToWhatsApp converts Markdown-formatted LLM output to WhatsApp's native
// formatting syntax. WhatsApp supports: *bold*, _italic_, ~strikethrough~, ```code```.
// Unsupported features are simplified: headers → bold, links → "text url", tables → plain.
func markdownToWhatsApp(text string) string {
	if text == "" {
		return ""
	}

	// Pre-process: convert HTML tags from LLM output to Markdown equivalents.
	text = htmlTagToWaMd(text)

	// Extract and protect fenced code blocks — WhatsApp renders ``` the same way.
	codeBlocks := waExtractCodeBlocks(text)
	text = codeBlocks.text

	// Headers (##, ###, etc.) → *bold text* (WhatsApp has no header concept).
	text = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`).ReplaceAllString(text, "*$1*")

	// Blockquotes → plain text.
	text = regexp.MustCompile(`(?m)^>\s*(.*)$`).ReplaceAllString(text, "$1")

	// Links [text](url) → "text url" (WhatsApp doesn't support markdown links).
	text = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(text, "$1 $2")

	// Bold: **text** or __text__ → *text*
	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "*$1*")
	text = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(text, "*$1*")

	// Strikethrough: ~~text~~ → ~text~
	text = regexp.MustCompile(`~~(.+?)~~`).ReplaceAllString(text, "~$1~")

	// Inline code: `code` → ```code``` (WhatsApp has no inline code, only blocks).
	text = regexp.MustCompile("`([^`]+)`").ReplaceAllString(text, "```$1```")

	// List items: leading - or * → bullet •
	text = regexp.MustCompile(`(?m)^[-*]\s+`).ReplaceAllString(text, "• ")

	// Restore code blocks as ``` … ``` preserving original content.
	for i, code := range codeBlocks.codes {
		// Trim trailing newline from extracted content — we add our own.
		code = strings.TrimRight(code, "\n")
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00CB%d\x00", i), "```\n"+code+"\n```")
	}

	// Collapse 3+ blank lines to 2.
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// htmlTagToWaMd converts common HTML tags in LLM output to Markdown equivalents
// so they are then processed by the markdown → WhatsApp pipeline above.
var htmlToWaMdReplacers = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)<br\s*/?>`), "\n"},
	{regexp.MustCompile(`(?i)</?p\s*>`), "\n"},
	{regexp.MustCompile(`(?i)<b>([\s\S]*?)</b>`), "**${1}**"},
	{regexp.MustCompile(`(?i)<strong>([\s\S]*?)</strong>`), "**${1}**"},
	{regexp.MustCompile(`(?i)<i>([\s\S]*?)</i>`), "_${1}_"},
	{regexp.MustCompile(`(?i)<em>([\s\S]*?)</em>`), "_${1}_"},
	{regexp.MustCompile(`(?i)<s>([\s\S]*?)</s>`), "~~${1}~~"},
	{regexp.MustCompile(`(?i)<strike>([\s\S]*?)</strike>`), "~~${1}~~"},
	{regexp.MustCompile(`(?i)<del>([\s\S]*?)</del>`), "~~${1}~~"},
	{regexp.MustCompile(`(?i)<code>([\s\S]*?)</code>`), "`${1}`"},
	{regexp.MustCompile(`(?i)<a\s+href="([^"]+)"[^>]*>([\s\S]*?)</a>`), "[${2}](${1})"},
}

func htmlTagToWaMd(text string) string {
	for _, r := range htmlToWaMdReplacers {
		text = r.re.ReplaceAllString(text, r.repl)
	}
	return text
}

type waCodeBlockMatch struct {
	text  string
	codes []string
}

// waExtractCodeBlocks pulls fenced code blocks out of text and replaces them with
// \x00CB{n}\x00 placeholders so other regex passes don't mangle their contents.
func waExtractCodeBlocks(text string) waCodeBlockMatch {
	re := regexp.MustCompile("```[\\w]*\\n?([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, m := range matches {
		codes = append(codes, m[1])
	}

	i := 0
	text = re.ReplaceAllStringFunc(text, func(_ string) string {
		placeholder := fmt.Sprintf("\x00CB%d\x00", i)
		i++
		return placeholder
	})

	return waCodeBlockMatch{text: text, codes: codes}
}

// mentionMatch represents a resolved @mention in outbound text.
type mentionMatch struct {
	original string // e.g., "@Alice Smith" or "@1234567890"
	phone    string // phone number for wire text: "1234567890"
	jid      string // full JID: "1234567890@s.whatsapp.net"
}

// mentionPattern matches @Name candidates.
// Captures @followed-by-word-chars, possibly with a second word (for "First Last").
// Uses \w+ instead of \S+ to avoid capturing trailing punctuation.
var mentionPattern = regexp.MustCompile(`(?:^|(?:\s))(@([\w]+(?:\s[\w]+)?))`)

// emailBeforeAt checks if the char before @ is a word char (email pattern).
var emailBeforeAt = regexp.MustCompile(`\w$`)

// resolveMentions scans text for @Name patterns and resolves them against known contacts.
// Returns the modified wire text (with @PhoneNumber) and a list of JIDs for MentionedJID.
// contacts is a list of known contacts for this channel instance.
func resolveMentions(text string, contacts []contactEntry) (string, []string) {
	if len(contacts) == 0 {
		return text, nil
	}

	var jids []string
	seen := make(map[string]bool)

	// Find all @candidates. Process longest matches first to handle "First Last" before "First".
	matches := mentionPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}

	// Process matches in reverse order to preserve indices during replacement.
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		// m[2]:m[3] is the full @mention (e.g., "@Alice Smith")
		// m[4]:m[5] is the name part (e.g., "Alice Smith")
		fullMatch := text[m[2]:m[3]]
		candidate := text[m[4]:m[5]]

		// Skip email patterns: if char before @ is a word char.
		if m[2] > 0 && emailBeforeAt.MatchString(text[m[2]-1:m[2]]) {
			continue
		}

		// Skip if inside a code block placeholder.
		if m[2] > 0 && text[m[2]-1] == '\x00' {
			continue
		}

		// Try to resolve: phone number first, then name lookup.
		resolved := resolveCandidate(candidate, contacts)
		if len(resolved) == 0 {
			// Try single word (first word only) if two-word match failed.
			parts := strings.SplitN(candidate, " ", 2)
			if len(parts) == 2 {
				resolved = resolveCandidate(parts[0], contacts)
				if len(resolved) > 0 {
					// Only matched first word — adjust the match boundary.
					fullMatch = "@" + parts[0]
					m[3] = m[2] + len(fullMatch)
				}
			}
		}
		if len(resolved) == 0 {
			continue
		}

		// Build replacement: @phone1 @phone2 (for multiple matches).
		var replaceParts []string
		for _, r := range resolved {
			if !seen[r.jid] {
				jids = append(jids, r.jid)
				seen[r.jid] = true
			}
			replaceParts = append(replaceParts, "@"+r.phone)
		}
		replacement := strings.Join(replaceParts, " ")
		text = text[:m[2]] + replacement + text[m[3]:]
	}

	return text, jids
}

// contactEntry is a simplified contact for mention resolution.
type contactEntry struct {
	displayName string // e.g., "Alice Smith"
	phone       string // e.g., "1234567890"
	jid         string // e.g., "1234567890@s.whatsapp.net"
}

// resolveCandidate tries to match a candidate string against contacts.
// Handles: pure phone numbers, exact name match, prefix name match.
func resolveCandidate(candidate string, contacts []contactEntry) []mentionMatch {
	// 1. Pure digits → phone number.
	if isDigits(candidate) {
		return []mentionMatch{{
			original: candidate,
			phone:    candidate,
			jid:      candidate + "@s.whatsapp.net",
		}}
	}

	// 2. Full JID.
	if strings.Contains(candidate, "@s.whatsapp.net") {
		phone := strings.SplitN(candidate, "@", 2)[0]
		return []mentionMatch{{original: candidate, phone: phone, jid: candidate}}
	}

	// 3. Name lookup — exact match first.
	candidateLower := strings.ToLower(candidate)
	var exact, prefix []mentionMatch
	for _, c := range contacts {
		nameLower := strings.ToLower(c.displayName)
		if nameLower == candidateLower {
			exact = append(exact, mentionMatch{original: candidate, phone: c.phone, jid: c.jid})
		} else if strings.HasPrefix(nameLower, candidateLower) {
			prefix = append(prefix, mentionMatch{original: candidate, phone: c.phone, jid: c.jid})
		}
	}
	if len(exact) > 0 {
		return exact
	}
	// Only use prefix match if exactly 1 result (avoid ambiguous mentions).
	if len(prefix) == 1 {
		return prefix
	}
	return nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// chunkText splits text into pieces that fit within maxLen,
// preferring to split at paragraph (\n\n) or line (\n) boundaries.
func chunkText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}
		// Find the best split point: paragraph > line > space > hard cut.
		cutAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n\n"); idx > 0 {
			cutAt = idx
		} else if idx := strings.LastIndex(text[:maxLen], "\n"); idx > 0 {
			cutAt = idx
		} else if idx := strings.LastIndex(text[:maxLen], " "); idx > 0 {
			cutAt = idx
		}
		chunks = append(chunks, strings.TrimRight(text[:cutAt], " \n"))
		text = strings.TrimLeft(text[cutAt:], " \n")
	}
	return chunks
}
