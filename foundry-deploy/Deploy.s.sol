// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "./src/StabilizationVault.sol";

contract DeployScript is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);

        vm.startBroadcast(deployerPrivateKey);

        // Config from config.yaml
        address token0 = 0x2d7EFFf683B0a21E0989729E0249C42cdF9eE442;  // GLUSD
        address token1 = 0x948E15B38f096d3a664fdeEf44C13709732B2110;  // USDT
        uint24 fee = 500;
        address factory = 0x1F98431c8aD98523631AE4a59f267346ea31F984;
        address swapRouter = 0xd1AAE39293221B77B0C71fBD6dCb7Ea29Bb5B166;

        StabilizationVault vault = new StabilizationVault(
            token0,
            token1,
            fee,
            factory,
            swapRouter,
            deployer  // Use deployer address explicitly
        );

        vm.stopBroadcast();

        console.log("StabilizationVault deployed to:", address(vault));
        console.log("Owner:", deployer);
    }
}
