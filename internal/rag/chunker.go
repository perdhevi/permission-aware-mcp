package rag

import "strings"

// Chunk uses bounded Unicode windows with overlap, retaining source text verbatim.
func Chunk(text string) []string {
	const size, overlap = 1000, 100
	runes := []rune(text)
	chunks := []string{}
	for start := 0; start < len(runes); start += size - overlap {
		end := min(start+size, len(runes))
		part := string(runes[start:end])
		if strings.TrimSpace(part) != "" {
			chunks = append(chunks, part)
		}
		if end == len(runes) {
			break
		}
	}
	return chunks
}
