package config

type UniswapContracts struct {
	Factory            string `yaml:"factory"`
	PositionManager    string `yaml:"position_manager"`
	SwapRouter         string `yaml:"swap_router"`
	Quoter             string `yaml:"quoter"`
	Multicall          string `yaml:"multicall"`
}

var UnichainSepolia = UniswapContracts{
	Factory:         "0x1F98431c8aD98523631AE4a59f267346ea31F984",
	PositionManager: "0xB7F724d6dDDFd008eFf5cc2834edDE5F9eF0d075",
	SwapRouter:      "0xd1AAE39293221B77B0C71fBD6dCb7Ea29Bb5B166",
	Quoter:          "0x6Dd37329A1A225a6Fca658265D460423DCafBF89",
	Multicall:       "0x9D0F15f2cf58655fDDcD1EE6129C547fDaeD01b1",
}

var Unichain = UniswapContracts{
	Factory:         "0x1f98400000000000000000000000000000000003",
	PositionManager: "0x943e6e07a7e8e791dafc44083e54041d743c46e9",
	SwapRouter:      "0x73855d06de49d0fe4a9c42636ba96c62da12ff9c",
	Quoter:          "0x385a5cf5f83e99f7bb2852b6a19c3538b9fa7658",
	Multicall:       "0xb7610f9b733e7d45184be3a1bc966960ccc54f0b",
}
