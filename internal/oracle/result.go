package oracle

// Violation is a trusted monitor finding bound to an execution step.
type Violation struct {
	Monitor string `json:"monitor"`
	Step    int    `json:"step,omitempty"`
	Message string `json:"message"`
}

// Result records which trusted monitors ran and the violations they emitted.
type Result struct {
	Checked    []string    `json:"checked"`
	Violations []Violation `json:"violations"`
}
