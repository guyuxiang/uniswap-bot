const { ethers } = require('ethers');
const fs = require('fs');

const RPC = "https://astrochain-sepolia.gateway.tenderly.co/5neqYQoinBsj3Cc3O36Dun";
const PK = "0x298149d01f7a23cb938ab6874ea345516479fb70bd5e14c99c0ffaf84798ca80";

// Read compiled artifact
const artifact = JSON.parse(fs.readFileSync('../hardhat-test/artifacts/contracts/StabilizationVault.sol/StabilizationVault.json', 'utf8'));

async function main() {
    const provider = new ethers.JsonRpcProvider(RPC);
    const wallet = new ethers.Wallet(PK, provider);
    
    console.log("Deploying from:", wallet.address);
    
    const factory = new ethers.ContractFactory(artifact.abi, artifact.bytecode, wallet);
    const contract = await factory.deploy(
        "0x2d7efff683b0a21e0989729e0249c42cdf9ee442", // token0
        "0x948e15b38f096d3a664fdeef44c13709732b2110", // token1
        100, // fee
        "0x1F98431c8aD98523631AE4a59f267346ea31F984", // factory
        "0xd1AAE39293221B77B0C71fBD6dCb7Ea29Bb5B166", // swapRouter
        wallet.address // owner
    );
    
    console.log("Tx:", contract.deploymentTransaction().hash);
    await contract.waitForDeployment();
    console.log("Deployed to:", await contract.getAddress());
}

main().catch(console.error);
