package obsidian

import (
	"regexp"
	"strconv"
	"strings"
)

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify genera un slug filesystem-safe: lowercase, no-alphanum→hyphen, max 60 chars + ID.
func Slugify(title string, id int64) string {
	if title == "" {
		return "observation-" + strconv.FormatInt(id, 10)
	}
	s := strings.ToLower(title)
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	return s + "-" + strconv.FormatInt(id, 10)
}
