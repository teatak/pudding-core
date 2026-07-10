// Package searchtext prepares user-facing text for full-text search.
package searchtext

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode"

	"github.com/teatak/seg/pkg/dict"
	segment "github.com/teatak/seg/pkg/seg"
)

var (
	//go:embed data/base.txt
	baseDictionary []byte

	//go:embed data/hmm.json
	hmmModel []byte

	defaultOnce      sync.Once
	defaultTokenizer *Tokenizer
	defaultErr       error
)

// Tokenizer wraps seg with script-boundary handling for unspaced mixed text.
type Tokenizer struct {
	segmenter *segment.Segmenter
}

// Prepare initializes the shared tokenizer and validates its embedded assets.
func Prepare() error {
	_, err := Default()
	return err
}

// Default returns the process-wide read-only tokenizer.
func Default() (*Tokenizer, error) {
	defaultOnce.Do(func() {
		defaultTokenizer, defaultErr = newTokenizer(baseDictionary, hmmModel)
	})
	return defaultTokenizer, defaultErr
}

func newTokenizer(dictionaryData, modelData []byte) (*Tokenizer, error) {
	dictionaryFile, err := os.CreateTemp("", "pudding-search-dict-*.txt")
	if err != nil {
		return nil, fmt.Errorf("create temporary search dictionary: %w", err)
	}
	dictionaryPath := dictionaryFile.Name()
	defer os.Remove(dictionaryPath)

	if _, err := dictionaryFile.Write(dictionaryData); err != nil {
		_ = dictionaryFile.Close()
		return nil, fmt.Errorf("write temporary search dictionary: %w", err)
	}
	if err := dictionaryFile.Close(); err != nil {
		return nil, fmt.Errorf("close temporary search dictionary: %w", err)
	}

	dictionary := dict.NewDictionary(dictionaryPath, "", "")
	if err := dictionary.Load(); err != nil {
		return nil, fmt.Errorf("load search dictionary: %w", err)
	}
	var hmm segment.HMM
	if err := json.Unmarshal(modelData, &hmm); err != nil {
		return nil, fmt.Errorf("load search HMM: %w", err)
	}
	return &Tokenizer{segmenter: segment.NewSegmenter(dictionary, &hmm, false)}, nil
}

// IndexText returns space-delimited terms suitable for an FTS5 unicode61 column.
func IndexText(text string) string {
	return strings.Join(Terms(text), " ")
}

// Terms preserves repeated terms so BM25 can retain term-frequency information.
func Terms(text string) []string {
	tokenizer, err := Default()
	if err != nil {
		return fallbackTerms(text)
	}
	return tokenizer.Terms(text)
}

// QueryTerms removes duplicates to keep generated MATCH expressions compact.
func QueryTerms(text string) []string {
	terms := Terms(text)
	seen := make(map[string]struct{}, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

// Terms separates Han, letter, and numeric runs before calling seg. This keeps
// strings such as "DeepSeek模型GPT4" searchable without requiring spaces.
func (t *Tokenizer) Terms(text string) []string {
	blocks := splitBlocks(text)
	out := make([]string, 0, len(blocks)*2)
	for _, block := range blocks {
		switch block.kind {
		case blockHan:
			for _, token := range t.segmenter.SegmentSearch(block.text) {
				term := normalizeTerm(token.Word)
				if term != "" {
					out = append(out, term)
				}
			}
		case blockAlpha, blockNumeric:
			if term := normalizeTerm(block.text); term != "" {
				out = append(out, term)
			}
		}
	}
	return out
}

type blockKind uint8

const (
	blockSeparator blockKind = iota
	blockHan
	blockAlpha
	blockNumeric
)

type textBlock struct {
	kind blockKind
	text string
}

func splitBlocks(text string) []textBlock {
	var blocks []textBlock
	var current []rune
	currentKind := blockSeparator
	flush := func() {
		if len(current) == 0 {
			return
		}
		blocks = append(blocks, textBlock{kind: currentKind, text: string(current)})
		current = nil
	}
	for _, r := range text {
		kind := classifyRune(r)
		if kind == blockSeparator {
			flush()
			currentKind = blockSeparator
			continue
		}
		if currentKind != kind {
			flush()
			currentKind = kind
		}
		current = append(current, r)
	}
	flush()
	return blocks
}

func classifyRune(r rune) blockKind {
	if unicode.Is(unicode.Han, r) {
		return blockHan
	}
	if unicode.IsLetter(r) {
		return blockAlpha
	}
	if unicode.IsDigit(r) {
		return blockNumeric
	}
	return blockSeparator
}

func normalizeTerm(term string) string {
	return strings.ToLower(strings.TrimSpace(term))
}

func fallbackTerms(text string) []string {
	blocks := splitBlocks(text)
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if term := normalizeTerm(block.text); term != "" {
			out = append(out, term)
		}
	}
	return out
}
