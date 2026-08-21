package targetoracles

import (
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

func EtcdraftV2Registry() Registry {
	return newRegistry(
		EtcdraftV2TargetID,
		etcdraftv2.DecisionProjectionID,
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "trace-integrity", Scope: controlexperiment.AgentOracleScopeGeneric,
			},
			Monitor: oracle.BundleTraceIntegrity{},
		},
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "agreement", Scope: controlexperiment.AgentOracleScopeGeneric,
				PropertyIDs: []string{"consensus-agreement"},
			},
			Monitor: oracle.BundleAgreement{},
		},
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: LogProgressMonitorID, Scope: controlexperiment.AgentOracleScopeTarget,
				PropertyIDs: []string{"applied-prefix-consistency", "recovery-progress-monotonicity"},
			},
			Monitor: LogProgressMonitor{},
		},
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: ElectionSafetyMonitorID, Scope: controlexperiment.AgentOracleScopeTarget,
				PropertyIDs: []string{"election-safety"},
			},
			Monitor: ElectionSafetyMonitor{},
		},
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: ClientApplicationBindingMonitorID, Scope: controlexperiment.AgentOracleScopeTarget,
				PropertyIDs: []string{"client-operation-continuity"},
			},
			Monitor: ClientApplicationBindingMonitor{},
		},
	)
}

func OmnipaxosV2Registry() Registry {
	return newRegistry(
		OmnipaxosV2TargetID,
		omnipaxosv2.DecisionProjectionID,
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "trace-integrity", Scope: controlexperiment.AgentOracleScopeGeneric,
			},
			Monitor: oracle.BundleTraceIntegrity{},
		},
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "agreement", Scope: controlexperiment.AgentOracleScopeGeneric,
				PropertyIDs: []string{"consensus-agreement", "decided-prefix-consistency"},
			},
			Monitor: oracle.BundleAgreement{},
		},
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID:          OmnipaxosClientDecisionBindingMonitorID,
				Scope:       controlexperiment.AgentOracleScopeTarget,
				PropertyIDs: []string{"client-operation-continuity"},
			},
			Monitor: OmnipaxosClientDecisionBindingMonitor{},
		},
	)
}
