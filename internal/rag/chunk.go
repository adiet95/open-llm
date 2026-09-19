package rag

import "strings"

// Chunk splits text into overlapping windows of about size runes, breaking on
// paragraph/sentence/word boundaries where possible so a chunk rarely cuts a
// word in half. overlap carries trailing context into the next chunk so a fact
// spanning a boundary is still retrievable. Both are measured in runes.
//
// This is intentionally simple and deterministic (no LLM) — good enough for
// most corpora and easy to reason about. Tune via RAG_CHUNK_SIZE / _OVERLAP.
func Chunk(text string, size, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if size <= 0 {
		size = 800
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 5
	}

	runes := []rune(text)
	if len(runes) <= size {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < len(runes) {
		end := start + size
		if end >= len(runes) {
			chunks = append(chunks, strings.TrimSpace(string(runes[start:])))
			break
		}

		// Prefer to break on a natural boundary within the last 30% of the window.
		breakAt := findBoundary(runes, start, end, size)
		chunk := strings.TrimSpace(string(runes[start:breakAt]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		// Advance, keeping `overlap` runes of trailing context.
		next := breakAt - overlap
		if next <= start { // guard against no forward progress
			next = breakAt
		}
		start = next
	}
	return chunks
}

// findBoundary looks backward from end for a paragraph break, then sentence,
// then whitespace, staying within the last ~30% of the window. Falls back to
// the hard end if nothing better is found.
func findBoundary(runes []rune, start, end, size int) int {
	minAcceptable := end - size*3/10
	if minAcceptable < start {
		minAcceptable = start
	}

	// Paragraph break "\n\n".
	for i := end - 1; i > minAcceptable; i-- {
		if runes[i] == '\n' && i-1 >= start && runes[i-1] == '\n' {
			return i + 1
		}
	}
	// Sentence end.
	for i := end - 1; i > minAcceptable; i-- {
		if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' || runes[i] == '\n' {
			return i + 1
		}
	}
	// Whitespace.
	for i := end - 1; i > minAcceptable; i-- {
		if runes[i] == ' ' || runes[i] == '\t' {
			return i + 1
		}
	}
	return end
}
