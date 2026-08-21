package main

import "github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"

func etcdraftCampaignRuntimeConfig() controlexperiment.RuntimeConfig {
	return controlexperiment.RuntimeConfig{
		SeedHex:   "6f6666696369616c2d65746364726166742d76322d636c75737465722d73656564",
		MaxClones: 1,
	}
}
