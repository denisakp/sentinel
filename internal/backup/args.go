package backup

import (
	"regexp"
)

// ParseAdditionalArgs parses additional arguments
func ParseAdditionalArgs(entry string) []string {
	re := regexp.MustCompile(`"[^"]*"|\S+`) // regex to match quoted strings or words
	return re.FindAllString(entry, -1)
}

// RemoveArgsDuplicate removes duplicate arguments from the list of arguments
func RemoveArgsDuplicate(args []string) []string {
	keys := make(map[string]bool) // map of arguments
	var list []string             // list of unique arguments

	for _, entry := range args {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}

	return list
}

// DefaultString sets the default value if the provided value is empty
func DefaultString(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}

	return value
}
