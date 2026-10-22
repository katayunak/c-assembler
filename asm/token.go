package asm

import (
	"fmt"
	"math"
	"unicode/utf8"
)

// Tokenizer counts tokens. Plug in an exact one when you have it.
type Tokenizer interface {
	Name() string
	Count(text string) int
	Exact() bool
}

// Estimator guesses tokens from text length. It is not exact.
type Estimator struct {
	CharsPerToken float64 // 0 means 3.5
}

func (e Estimator) ratio() float64 {
	if e.CharsPerToken > 0 {
		return e.CharsPerToken
	}
	return 3.5
}

func (e Estimator) Name() string { return fmt.Sprintf("estimate (%.2g chars/token)", e.ratio()) }
func (e Estimator) Exact() bool  { return false }

func (e Estimator) Count(text string) int {
	return int(math.Ceil(float64(utf8.RuneCountInString(text)) / e.ratio()))
}
