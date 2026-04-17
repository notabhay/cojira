package meta

import "testing"

func FuzzParseIntent(f *testing.F) {
	seeds := []string{
		"who am i",
		"close PROJ-123",
		`comment on PROJ-123 "please review"`,
		`comment on page 6573916430 "looks good"`,
		`create confluence page titled "Architecture" in CAIS under 6573916430`,
		"delete issue PROJ-123",
		"delete page 6573916430",
		"archive page 6573916430 under 6573916431 label archived",
		"show board 12345",
		"do something weird and random",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		args := parseIntent(input)
		if args == nil {
			return
		}
		if len(args) < 2 {
			t.Fatalf("parsed command too short: %#v", args)
		}
		if args[0] != "jira" && args[0] != "confluence" {
			t.Fatalf("unexpected root command: %#v", args)
		}
	})
}
