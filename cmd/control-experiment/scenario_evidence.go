package main

import (
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func latestScenarioEvidence(trace controlruntime.Trace) control.EvidenceEnvelope {
	for index := len(trace.Records) - 1; index >= 0; index-- {
		if trace.Records[index].Evidence != nil {
			return *trace.Records[index].Evidence
		}
	}
	return trace.InitialEvidence
}
