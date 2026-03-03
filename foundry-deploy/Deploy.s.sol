// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";

contract DeployScript is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");
        
        vm.startBroadcast(deployerPrivateKey);
        
        // Config from config.yaml
        address token0 = 0x2d7efff683b0a21e0989729e0249c42cdf9ee442;  // GLUSD
        address token1 = 0x948e15b38f096d3a664fdeef44c13709732b2110;  // USDT
        uint24 fee = 500;
        address factory = 0x1F98431c8aD98523631AE4a59f267346ea31F984;
        address swapRouter = 0xd1AAE39293221B77B0C71fBD6dCb7Ea29Bb5B166;
        
        StabilizationVault vault = new StabilizationVault(
            token0,
            token1,
            fee,
            factory,
            swapRouter,
            msg.sender
        );
        
        vm.stopBroadcast();
        
        console.log("StabilizationVault deployed to:", address(vault));
    }
}
