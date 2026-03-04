// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/StabilizationVault.sol";

contract DeployScript is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");
        
        vm.startBroadcast(deployerPrivateKey);
        
        StabilizationVault vault = new StabilizationVault(
            0x2d7EFFf683B0a21E0989729E0249C42cdF9eE442,
            0x948E15b38f096d3a664fdeEf44C13709732B2110,
            500,
            0x1F98431c8aD98523631AE4a59f267346ea31F984,
            0xd1AAE39293221B77B0C71fBD6dCb7Ea29Bb5B166,
            msg.sender
        );
        
        vm.stopBroadcast();
        
        console.log("StabilizationVault deployed to:", address(vault));
    }
}
