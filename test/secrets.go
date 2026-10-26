package test

import (
	"embed"
	"io/fs"
)

//go:embed fixtures/secrets
var secretFixtures embed.FS

// SecretFixtures holds credential-shaped samples: positive/<rule-id>.txt must
// raise that rule, negative/<rule-id>.txt must not, clean.txt raises nothing.
// Every credential in it is synthetic.
func SecretFixtures() fs.FS {
	sub, err := fs.Sub(secretFixtures, "fixtures/secrets")
	if err != nil {
		panic(err)
	}
	return sub
}
