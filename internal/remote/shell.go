package remote

import (
	"regexp"
	"strings"
)

var safeSID = regexp.MustCompile(`^[a-f0-9]{8,32}$`)

func SingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func SafeSID(sid string) bool {
	return safeSID.MatchString(sid)
}
