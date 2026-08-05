package coverage

import (
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

type Profile struct {
	Version   int                `json:"version"`
	ID        string             `json:"id"`
	Protocol  string             `json:"protocol"`
	Nodes     []string           `json:"nodes"`
	Coverage  CoverageDefinition `json:"coverage"`
	Threshold Threshold          `json:"threshold"`
}

type CoverageDefinition struct {
	Weights map[string]float64 `json:"weights"`
	Atoms   []Atom             `json:"atoms"`
}

type Threshold struct {
	Score       float64 `json:"score"`
	MinCategory float64 `json:"min_category"`
}

type Atom struct {
	ID           string `json:"id"`
	Category     string `json:"category"`
	Description  string `json:"description"`
	WitnessLabel string `json:"witness_label"`
	Monitor      string `json:"monitor"`
	Status       string `json:"status"`
}

type Evidence struct {
	Reach   bool `json:"reach"`
	Observe bool `json:"observe"`
	Check   bool `json:"check"`
	Replay  bool `json:"replay"`
	Conform bool `json:"conform"`
}

func (e Evidence) Strong() bool {
	return e.Reach && e.Observe && e.Check && e.Replay && e.Conform
}

type AtomResult struct {
	Atom     Atom     `json:"atom"`
	Evidence Evidence `json:"evidence"`
	Covered  bool     `json:"covered"`
	Step     int      `json:"witness_step,omitempty"`
}

type CategoryResult struct {
	Category string  `json:"category"`
	Covered  int     `json:"covered"`
	Total    int     `json:"total"`
	Ratio    float64 `json:"ratio"`
	Weight   float64 `json:"weight"`
}

type Summary struct {
	Score      float64          `json:"score"`
	High       bool             `json:"high"`
	Categories []CategoryResult `json:"categories"`
	Atoms      []AtomResult     `json:"atoms"`
}

func (p Profile) Validate() error {
	if p.Version != 1 {
		return fmt.Errorf("unsupported profile version %d", p.Version)
	}
	if p.ID == "" || p.Protocol == "" || len(p.Nodes) == 0 {
		return errors.New("profile id, protocol, and nodes are required")
	}
	weightSum := 0.0
	for _, weight := range p.Coverage.Weights {
		if weight < 0 {
			return errors.New("coverage weights cannot be negative")
		}
		weightSum += weight
	}
	if weightSum < 0.999 || weightSum > 1.001 {
		return fmt.Errorf("coverage weights must sum to 1, got %.4f", weightSum)
	}
	seen := make(map[string]bool)
	for _, atom := range p.Coverage.Atoms {
		if atom.ID == "" || atom.Category == "" || atom.WitnessLabel == "" {
			return errors.New("each coverage atom needs id, category, and witness_label")
		}
		if seen[atom.ID] {
			return fmt.Errorf("duplicate coverage atom %s", atom.ID)
		}
		seen[atom.ID] = true
		if _, ok := p.Coverage.Weights[atom.Category]; !ok {
			return fmt.Errorf("atom %s uses category without weight: %s", atom.ID, atom.Category)
		}
		if atom.Status != "supported" && atom.Status != "unsupported" {
			return fmt.Errorf("atom %s has invalid status %q", atom.ID, atom.Status)
		}
	}
	return nil
}

func Evaluate(profile Profile, trace []core.TraceRecord, checked oracle.Result, replay, conform bool) Summary {
	labels := make(map[string]int)
	for _, record := range trace {
		for _, observation := range record.Observations {
			if _, exists := labels[observation.Label]; !exists {
				labels[observation.Label] = record.Step
			}
		}
	}
	checkedMonitors := make(map[string]bool)
	for _, name := range checked.Checked {
		checkedMonitors[name] = true
	}

	counts := make(map[string]*CategoryResult)
	summary := Summary{}
	for _, atom := range profile.Coverage.Atoms {
		step, reached := labels[atom.WitnessLabel]
		monitorChecked := atom.Monitor == "" || checkedMonitors[atom.Monitor]
		evidence := Evidence{
			Reach:   reached,
			Observe: reached,
			Check:   monitorChecked,
			Replay:  replay,
			Conform: conform,
		}
		covered := atom.Status == "supported" && evidence.Strong()
		summary.Atoms = append(summary.Atoms, AtomResult{
			Atom: atom, Evidence: evidence, Covered: covered, Step: step,
		})
		category := counts[atom.Category]
		if category == nil {
			category = &CategoryResult{Category: atom.Category, Weight: profile.Coverage.Weights[atom.Category]}
			counts[atom.Category] = category
		}
		category.Total++
		if covered {
			category.Covered++
		}
	}

	minimum := 1.0
	for _, category := range counts {
		if category.Total > 0 {
			category.Ratio = float64(category.Covered) / float64(category.Total)
		}
		summary.Score += 100 * category.Weight * category.Ratio
		if category.Ratio < minimum {
			minimum = category.Ratio
		}
		summary.Categories = append(summary.Categories, *category)
	}
	sort.Slice(summary.Categories, func(i, j int) bool {
		return summary.Categories[i].Category < summary.Categories[j].Category
	})
	summary.High = summary.Score >= profile.Threshold.Score && minimum >= profile.Threshold.MinCategory
	return summary
}
