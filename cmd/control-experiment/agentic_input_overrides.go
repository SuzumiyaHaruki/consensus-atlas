package main

// agenticInputOverrides keeps topology in the run configuration rather than
// requiring a copy of stable protocol material. Zero preserves the semantic
// input's configured membership.
type agenticInputOverrides struct {
	NodeCount int
}

func (overrides agenticInputOverrides) validate() bool {
	return overrides.NodeCount >= 0
}
