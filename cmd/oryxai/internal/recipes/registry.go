package recipes

// Registry of every supported agent. Each recipe is a pure value
// (no state) so we can list them once and iterate.
//
// Adding a new agent: implement Recipe (Name, DisplayName, Detect,
// Install, Uninstall, Mode), then add the value here. Nothing else
// in the binary needs to change.

import (
	"sort"
)

// All returns every registered recipe. Order is stable.
func All() []Recipe {
	out := []Recipe{
		ClaudeCodeRecipe{},
		CursorRecipe{},
		ClineRecipe{},
		ContinueRecipe{},
		CodyRecipe{},
		CodeiumRecipe{},
		GeminiCLIRecipe{},
		OpenCodeRecipe{},
		OpenClawRecipe{},
		WindsurfRecipe{},
		KiloRecipe{},
		AntigravityRecipe{},
		CodexRecipe{},
		CopilotCLIRecipe{},
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

// ByName returns a recipe by its kebab-case name, or nil if unknown.
func ByName(name string) Recipe {
	for _, r := range All() {
		if r.Name() == name {
			return r
		}
	}
	return nil
}

// Names returns the kebab-case identifier for every registered recipe.
// Used in `oryxai install --help` output.
func Names() []string {
	rs := All()
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name()
	}
	return out
}
