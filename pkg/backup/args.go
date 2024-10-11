package backup

import (
	"regexp"
)

// ParseAdditionalArgs parses additional arguments
func ParseAdditionalArgs(entry string) []string {
	re := regexp.MustCompile(`"[^"]*"|\S+`)
	return re.FindAllString(entry, -1)
}

// RemoveArgsDuplicate removes duplicate arguments
func RemoveArgsDuplicate(args []string) []string {
	keys := make(map[string]bool)
	var list []string

	for _, entry := range args {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}
