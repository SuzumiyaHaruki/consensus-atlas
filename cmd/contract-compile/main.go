package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

type output struct {
	ContractDigest  string           `json:"contract_digest"`
	ObligationCount int              `json:"obligation_count"`
	DeclaredProfile coverage.Profile `json:"declared_profile"`
}

func main() {
	contractPath := flag.String("contract", "contracts/etcdraft-v1.json", "protocol knowledge contract JSON")
	flag.Parse()
	contract, err := protocolcontract.Load(*contractPath)
	if err != nil {
		fail(err)
	}
	if err := contract.Validate(); err != nil {
		fail(err)
	}
	digest, err := protocolcontract.Digest(contract)
	if err != nil {
		fail(err)
	}
	declared := make(map[string]bool, len(contract.Capabilities))
	for _, capability := range contract.Capabilities {
		declared[capability.ID] = true
	}
	profile, err := protocolcontract.Compile(contract, declared)
	if err != nil {
		fail(err)
	}
	encoded, err := json.MarshalIndent(output{
		ContractDigest: digest, ObligationCount: len(contract.Obligations()), DeclaredProfile: profile,
	}, "", "  ")
	if err != nil {
		fail(err)
	}
	os.Stdout.Write(append(encoded, '\n'))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "contract-compile:", err)
	os.Exit(1)
}
