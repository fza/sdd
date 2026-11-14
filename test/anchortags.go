package test

import "regexp"

// placedTag is an anchor tag as sdd places it in served text: before a word,
// with a space between. A held piece's pointer
// names its tag after the name instead, and keeps it.
var placedTag = regexp.MustCompile(`⟦[0-9a-z]{4}⟧ (\pL)`)

// Untagged returns served text with every placed anchor tag taken out, so an
// assertion on a phrase holds wherever the random tag landed.
func Untagged(text string) string { return placedTag.ReplaceAllString(text, "$1") }
