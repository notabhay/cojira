package board

import "testing"

func FuzzResolveBoardIdentifier(f *testing.F) {
	seeds := []string{
		"45434",
		"https://jira.example.com/jira/secure/RapidView.jspa?rapidView=45434",
		"https://jira.example.com/jira/software/c/projects/PROJ/boards/1234",
		"rapidView=999",
		"",
		"myboard",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = ResolveBoardIdentifier(input)
	})
}
