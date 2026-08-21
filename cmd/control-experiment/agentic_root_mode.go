package main

const (
	agenticRootBootstrap     = "bootstrap"
	agenticRootWorkloadReady = "workload-ready"
)

func validAgenticRootMode(mode string) bool {
	return mode == agenticRootBootstrap || mode == agenticRootWorkloadReady
}

// agenticInputOverrides keeps topology and root selection in the run
// configuration rather than requiring a copy of the stable protocol material.
// Zero values preserve the semantic input.
type agenticInputOverrides struct {
	NodeCount int
	RootMode  string
}

func (overrides agenticInputOverrides) validate() bool {
	return overrides.NodeCount >= 0 &&
		(overrides.RootMode == "" || validAgenticRootMode(overrides.RootMode))
}
