// Package argv provides flag parsing helpers for the cojira CLI,
// including consuming leading flags and reordering known flags.
package argv

import "strings"

// NetworkFlagArity maps tool-level network flags to their arity
// (number of arguments they consume: 0 for boolean, 1 for value).
var NetworkFlagArity = map[string]int{
	"--timeout":          1,
	"--retries":          1,
	"--retry-base-delay": 1,
	"--retry-max-delay":  1,
	"--debug":            0,
}

// ConsumeLeadingFlags extracts known flags (and their values) from the beginning
// of argv. It stops at the first unknown token, a stop-token, or "--".
// Returns (collected flags, remaining argv).
func ConsumeLeadingFlags(argv []string, flagArity map[string]int, stopTokens []string) (collected []string, rest []string) {
	stop := make(map[string]struct{}, len(stopTokens))
	for _, s := range stopTokens {
		stop[s] = struct{}{}
	}

	i := 0
	for i < len(argv) {
		token := argv[i]

		// Stop tokens
		if _, ok := stop[token]; ok {
			break
		}
		// Double-dash separator
		if token == "--" {
			break
		}

		// --flag=value for arity-1 flags
		if strings.HasPrefix(token, "--") && strings.Contains(token, "=") {
			opt := strings.SplitN(token, "=", 2)[0]
			if arity, ok := flagArity[opt]; ok && arity == 1 {
				collected = append(collected, token)
				i++
				continue
			}
		}

		// Known flag
		if arity, ok := flagArity[token]; ok {
			collected = append(collected, token)
			if arity == 1 && i+1 < len(argv) {
				collected = append(collected, argv[i+1])
				i += 2
			} else {
				i++
			}
			continue
		}

		// Unknown token — stop consuming
		break
	}

	rest = argv[i:]
	return collected, rest
}

// ReorderKnownFlags moves known flags (and their values) to the front of argv,
// preserving the relative order of both known and unknown tokens.
// Everything after "--" is left untouched.
func ReorderKnownFlags(argv []string, flagArity map[string]int) []string {
	var collected []string
	var rest []string

	i := 0
	for i < len(argv) {
		token := argv[i]

		// Double-dash separator: everything from here is passthrough
		if token == "--" {
			rest = append(rest, argv[i:]...)
			break
		}

		// --flag=value for arity-1 flags
		if strings.HasPrefix(token, "--") && strings.Contains(token, "=") {
			opt := strings.SplitN(token, "=", 2)[0]
			if arity, ok := flagArity[opt]; ok && arity == 1 {
				collected = append(collected, token)
				i++
				continue
			}
		}

		// Known flag
		if arity, ok := flagArity[token]; ok {
			collected = append(collected, token)
			if arity == 1 && i+1 < len(argv) {
				collected = append(collected, argv[i+1])
				i += 2
			} else {
				i++
			}
			continue
		}

		// Unknown token — goes to rest
		rest = append(rest, token)
		i++
	}

	return append(collected, rest...)
}
