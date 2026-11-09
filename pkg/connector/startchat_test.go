package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseIdentifier(t *testing.T) {
	for input, expected := range map[string]struct {
		kind  identifierKind
		value string
	}{
		"alice":                       {identifierUsername, "alice"},
		"@alice":                      {identifierUsername, "alice"},
		" alice ":                     {identifierUsername, "alice"},
		"alice@example.org":           {identifierEmail, "alice@example.org"},
		"9ngpm3ajkjgb7qbtoq9d8pyu4y":  {identifierUserID, "9ngpm3ajkjgb7qbtoq9d8pyu4y"},
		"9ngpm3ajkjgb7qbtoq9d8pyu4":   {identifierUsername, "9ngpm3ajkjgb7qbtoq9d8pyu4"},
		"@9ngpm3ajkjgb7qbtoq9d8pyu4y": {identifierUsername, "9ngpm3ajkjgb7qbtoq9d8pyu4y"},
	} {
		kind, value := parseIdentifier(input)
		assert.Equal(t, expected.kind, kind, input)
		assert.Equal(t, expected.value, value, input)
	}
}
