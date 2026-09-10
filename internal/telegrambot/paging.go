package telegrambot

import "strings"

// maxMessageLen is comfortably under Telegram's 4096-character message limit,
// leaving headroom for formatting.
const maxMessageLen = 3500

// paginate joins lines with newlines into as few messages as possible, each no
// longer than limit characters. A single line longer than limit still becomes
// its own (over-long) message rather than being split mid-line.
func paginate(lines []string, limit int) []string {
	if len(lines) == 0 {
		return nil
	}
	var pages []string
	var cur strings.Builder
	for _, line := range lines {
		if cur.Len() > 0 && cur.Len()+1+len(line) > limit {
			pages = append(pages, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		pages = append(pages, cur.String())
	}
	return pages
}
