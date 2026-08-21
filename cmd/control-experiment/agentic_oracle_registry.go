package main

import (
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

func etcdraftAgenticOracleRegistry() targetoracles.Registry {
	return targetoracles.EtcdraftV2Registry()
}

func omnipaxosAgenticOracleRegistry() targetoracles.Registry {
	return targetoracles.OmnipaxosV2Registry()
}
