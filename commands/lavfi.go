package commands

import "strings"

// escapeFilterValue makes s safe as a filter option value inside a filtergraph
// string, e.g. the file name in movie=. ffmpeg unescapes twice: once for the
// filtergraph (\ ' [ ] , ;) and once for the option value (\ ' :).
// see https://ffmpeg.org/ffmpeg-filters.html#Notes-on-filtergraph-escaping
func escapeFilterValue(s string) string {
	return escapeChars(escapeChars(s, `\':`), `\'[],;`)
}

func escapeChars(s, special string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// safeOutPath keeps a relative path that starts with '-' from being read as
// an option where ffmpeg takes a bare output file name.
func safeOutPath(p string) string {
	if strings.HasPrefix(p, "-") {
		return "./" + p
	}
	return p
}
