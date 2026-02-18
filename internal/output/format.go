package output

import (
	"encoding/json"
	"fmt"
	"os"
)

// JSONDumps serialises data to a pretty-printed JSON string.
func JSONDumps(data any) (string, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// PrintJSON serialises data to pretty-printed JSON and writes it to stdout.
func PrintJSON(data any) error {
	s, err := JSONDumps(data)
	if err != nil {
		return err
	}
	fmt.Println(s)
	return nil
}

// EmitProgress writes a progress indicator to stderr.
// In JSON mode it writes a JSON object; in human mode a bracket-style line.
// In summary mode or when quiet is true, nothing is emitted.
func EmitProgress(mode string, quiet bool, index, total int, message string, status string) {
	if mode == "summary" {
		return
	}
	if quiet {
		return
	}
	if mode == "json" {
		payload := map[string]any{
			"type":    "progress",
			"index":   index,
			"total":   total,
			"message": message,
			"status":  status,
		}
		b, _ := json.Marshal(payload)
		fmt.Fprintln(os.Stderr, string(b))
		return
	}
	suffix := ""
	if status != "" {
		suffix = " " + status
	}
	fmt.Fprintf(os.Stderr, "[%d/%d] %s%s\n", index, total, message, suffix)
}
