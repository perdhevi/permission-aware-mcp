package rag

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"
	"unicode"
)

const Dimensions = 128
const Model = "dev-hash-v1"

// HashEmbedder is an offline lexical demo, not a language embedding model.
type HashEmbedder struct{}

func (HashEmbedder) Model() string   { return Model }
func (HashEmbedder) Dimensions() int { return Dimensions }
func (HashEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v := make([]float32, Dimensions)
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		v[int(h.Sum32()%Dimensions)]++
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return nil, fmt.Errorf("text has no searchable terms")
	}
	for i := range v {
		v[i] /= float32(math.Sqrt(norm))
	}
	return v, nil
}
func (h HashEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		v, err := h.EmbedQuery(ctx, text)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// VectorLiteral validates provider output before it crosses the storage boundary.
func VectorLiteral(v []float32, dimensions int) (string, error) {
	if len(v) != dimensions || dimensions < 1 {
		return "", fmt.Errorf("invalid vector dimensions")
	}
	parts := make([]string, len(v))
	nonzero := false
	for i, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return "", fmt.Errorf("nonfinite vector")
		}
		nonzero = nonzero || x != 0
		parts[i] = strconv.FormatFloat(float64(x), 'g', -1, 32)
	}
	if !nonzero {
		return "", fmt.Errorf("zero vector")
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}
