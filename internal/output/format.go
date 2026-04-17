package output

import (
	"encoding/json"
	"fmt"
	"os"
)

// JSONDumps serialises data to a pretty-printed JSON string.
func JSONDumps(data any) (string, error) {
	data = ApplySelect(data)
	var (
		b   []byte
		err error
	)
	if GetMode() == "ndjson" {
		b, err = json.Marshal(data)
	} else {
		b, err = json.MarshalIndent(data, "", "  ")
	}
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
	if quiet {
		return
	}
	EmitEvent("progress", map[string]any{
		"index":   index,
		"total":   total,
		"message": message,
		"status":  status,
	})
	if mode == "summary" {
		return
	}
	if mode == "json" || mode == "ndjson" {
		payload := map[string]any{
			"type":      "progress",
			"stream_id": CurrentEventStreamID(),
			"timestamp": UTCNowISO(),
			"index":     index,
			"total":     total,
			"message":   message,
			"status":    status,
		}
		b, _ := json.Marshal(payload)
		fmt.Fprintln(os.Stderr, string(b))
		return
	}
	suffix := ""
	if status != "" {
		label := status
		if ShouldColorize() {
			label = colorizeStatus(status)
		}
		suffix = " " + label
	}
	percent := 0
	if total > 0 {
		percent = int(float64(index) * 100 / float64(total))
	}
	fmt.Fprintf(os.Stderr, "[%d/%d %3d%%] %s%s\n", index, total, percent, message, suffix)
}
