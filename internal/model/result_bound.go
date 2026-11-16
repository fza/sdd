package model

// MaxResultChars bounds the text content of one MCP result, for every host:
// Codex prints a result's JSON within a 10,000-token output budget and drops
// the middle of a larger one, past about 19,000 characters of text. Claude
// Code saves a result past 50,000 characters to a file.
const MaxResultChars = 18000

// MaxServeChars is what one serve may take of a result, leaving room for the
// notices that join it.
const MaxServeChars = MaxResultChars - 2000
