package processing

import "strings"

const (
	ThinkStart = "<think>"
	ThinkEnd   = "</think>"
)

// ExtractThinking processes a full string and separates thinking content from the rest.
// It handles multiple <think>...</think> blocks.
func ExtractThinking(text string) (content string, reasoning string) {
	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder

	cursor := 0
	length := len(text)

	for cursor < length {
		// find the start of think block
		startIdx := strings.Index(text[cursor:], ThinkStart)
		if startIdx == -1 {
			// therere no more think blocks
			contentBuilder.WriteString(text[cursor:])
			break
		}

		realStart := cursor + startIdx
		contentBuilder.WriteString(text[cursor:realStart])

		// move the cursor to inside the tag
		cursor = realStart + len(ThinkStart)

		// find the end of think block
		endIdx := strings.Index(text[cursor:], ThinkEnd)
		if endIdx == -1 {
			// no end tag found, assume rest is reasoning (or malformed)
			reasoningBuilder.WriteString(text[cursor:])
			break
		}

		realEnd := cursor + endIdx
		reasoningBuilder.WriteString(text[cursor:realEnd])

		// move cursor past the end tag
		cursor = realEnd + len(ThinkEnd)
	}

	return contentBuilder.String(), reasoningBuilder.String()
}

type StreamParser struct {
	inBlock bool
	buffer  string
}

func NewStreamParser() *StreamParser {
	return &StreamParser{}
}

// Process takes a chunk of text and returns the separated content and reasoning parts.
// It maintains state to handle tags split across chunks.
func (p *StreamParser) Process(input string) (content string, reasoning string) {
	text := p.buffer + input
	p.buffer = ""

	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder

	cursor := 0
	length := len(text)

	for cursor < length {
		if !p.inBlock {
			// look for the start tag
			idx := strings.Index(text[cursor:], ThinkStart)
			if idx != -1 {
				// weve found the tag
				realIdx := cursor + idx
				contentBuilder.WriteString(text[cursor:realIdx])
				cursor = realIdx + len(ThinkStart)
				p.inBlock = true
			} else {
				// we need to check for partial tag at the end
				// this would be a suffix of text that is a prefix of `ThinkStart``
				matchedPartial := false

				// we check from largest possible partial (len(ThinkStart)-1) down to 1
				maxPartial := len(ThinkStart) - 1
				if len(text[cursor:]) < maxPartial {
					maxPartial = len(text[cursor:])
				}

				for i := maxPartial; i > 0; i-- {
					suffix := text[length-i:]
					if strings.HasPrefix(ThinkStart, suffix) {
						contentBuilder.WriteString(text[cursor : length-i])
						p.buffer = suffix
						cursor = length
						matchedPartial = true
						break
					}
				}

				if !matchedPartial {
					contentBuilder.WriteString(text[cursor:])
					cursor = length
				}
			}
		} else {
			// inside block, look for end tag
			idx := strings.Index(text[cursor:], ThinkEnd)
			if idx != -1 {
				realIdx := cursor + idx
				reasoningBuilder.WriteString(text[cursor:realIdx])
				cursor = realIdx + len(ThinkEnd)
				p.inBlock = false
			} else {
				// chcek for partial end tag
				matchedPartial := false

				maxPartial := len(ThinkEnd) - 1
				if len(text[cursor:]) < maxPartial {
					maxPartial = len(text[cursor:])
				}

				for i := maxPartial; i > 0; i-- {
					suffix := text[length-i:]
					if strings.HasPrefix(ThinkEnd, suffix) {
						reasoningBuilder.WriteString(text[cursor : length-i])
						p.buffer = suffix
						cursor = length
						matchedPartial = true
						break
					}
				}

				if !matchedPartial {
					reasoningBuilder.WriteString(text[cursor:])
					cursor = length
				}
			}
		}
	}

	return contentBuilder.String(), reasoningBuilder.String()
}
