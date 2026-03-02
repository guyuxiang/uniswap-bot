// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract ReentrancyGuard { uint256 private _status = 1; modifier nonReentrant() { require(_status == 1); _status = 2; _; _status = 1; } }
abstract contract Ownable { address public owner; constructor(address _owner) { owner = _owner; } modifier onlyOwner() { require(msg.sender == owner); _; } }

interface IERC20 { function transfer(address, uint256) external returns (bool); function transferFrom(address, address, uint256) external returns (bool); function balanceOf(address) external view returns (uint256); function approve(address, uint256) external returns (bool); }

library SafeERC20 {
    function sT(IERC20 t, address to, uint256 a) internal { (bool s,) = address(t).call(abi.encodeWithSignature("transfer(address,uint256)",to,a)); require(s); }
    function sTF(IERC20 t, address f, address to, uint256 a) internal { (bool s,) = address(t).call(abi.encodeWithSignature("transferFrom(address,address,uint256)",f,to,a)); require(s); }
    function fA(IERC20 t, address s, uint256 v) internal { (bool s,) = address(t).call(abi.encodeWithSignature("approve(address,uint256)",s,v)); require(s); }
}

interface IUniswapV3Pool { function slot0() external view returns (uint160, int24, uint16, uint16, uint16, uint8, bool); }
interface IUniswapV3Factory { function getPool(address, address, uint24) external view returns (address); }
interface ISwapRouter { struct EISP { address tI; address tO; uint24 fee; address rec; uint256 dl; uint256 aI; uint256 aOM; uint160 sPL; } function exactInputSingle(EISP calldata) external payable returns (uint256); }

contract StabilizationVault is Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;
    address public token0; address public token1; uint256 public reserve0; uint256 public reserve1;
    IUniswapV3Pool public pool; ISwapRouter public swapRouter;
    uint24 public fee; uint256 public rebalanceThresholdBps = 20; uint256 public targetPrice;
    bool public circuitBreakerActive = false; address public factory;

    event Deposit(address,uint256,uint256); event Withdraw(address,uint256,uint256);
    event ArbitrageExecuted(uint256,uint256,bool); event CircuitBreakerTriggered(string); event ParametersUpdated(string,uint256);

    constructor(address _token0, address _token1, uint24 _fee, address _factory, address _swapRouter, address _owner) Ownable(_owner) {
        token0 = _token0; token1 = _token1; factory = _factory;
        pool = IUniswapV3Pool(IUniswapV3Factory(_factory).getPool(token0, token1, _fee));
        fee = _fee; swapRouter = ISwapRouter(_swapRouter); targetPrice = 1;
    }

    function deposit(uint256 amount0, uint256 amount1) external onlyOwner nonReentrant {
        require(amount0 > 0 || amount1 > 0);
        if (amount0 > 0) IERC20(token0).sTF(msg.sender, address(this), amount0);
        if (amount1 > 0) IERC20(token1).sTF(msg.sender, address(this), amount1);
        reserve0 += amount0; reserve1 += amount1;
        emit Deposit(msg.sender, amount0, amount1);
    }

    function withdraw(uint256 amount0, uint256 amount1) external onlyOwner nonReentrant {
        require(amount0 > 0 || amount1 > 0);
        require(amount0 <= reserve0 && amount1 <= reserve1);
        reserve0 -= amount0; reserve1 -= amount1;
        if (amount0 > 0) IERC20(token0).sT(msg.sender, amount0);
        if (amount1 > 0) IERC20(token1).sT(msg.sender, amount1);
        emit Withdraw(msg.sender, amount0, amount1);
    }

    function executeArbitrage() external onlyOwner nonReentrant returns (bool) {
        require(!circuitBreakerActive);
        (uint160 sqrtPriceX96, ) = pool.slot0();
        uint256 currentPrice = (uint256(sqrtPriceX96) ** 2) >> 192;
        uint256 priceDiff = currentPrice > targetPrice ? currentPrice - targetPrice : targetPrice - currentPrice;
        uint256 deviationBps = (priceDiff * 10000) / targetPrice;
        require(deviationBps >= rebalanceThresholdBps, "Price deviation below threshold");
        
        bool zeroForOne = currentPrice > targetPrice;
        address tokenIn = zeroForOne ? token0 : token1;
        address tokenOut = zeroForOne ? token1 : token0;
        
        uint256 balanceInBefore = IERC20(tokenIn).balanceOf(address(this));
        require(balanceInBefore > 0, "No balance");
        
        IERC20(tokenIn).fA(address(swapRouter), balanceInBefore);
        
        uint256 amountOut = ISwapRouter(address(swapRouter)).exactInputSingle(ISwapRouter.EISP({
            tI: tokenIn, tO: tokenOut, fee: fee, rec: address(this), dl: block.timestamp, aI: balanceInBefore, aOM: 0, sPL: 0
        }));
        
        require(amountOut > 0, "No output");
        
        uint256 balanceInAfter = IERC20(tokenIn).balanceOf(address(this));
        uint256 balanceOutAfter = IERC20(tokenOut).balanceOf(address(this));
        
        if (zeroForOne) { reserve0 = balanceInAfter; reserve1 = balanceOutAfter; }
        else { reserve0 = balanceOutAfter; reserve1 = balanceInAfter; }
        
        emit ArbitrageExecuted(balanceInBefore, amountOut, true);
        return true;
    }

    function triggerCircuitBreaker(string calldata reason) external onlyOwner { circuitBreakerActive = true; emit CircuitBreakerTriggered(reason); }
    function releaseCircuitBreaker() external onlyOwner { circuitBreakerActive = false; }
    function setRebalanceThresholdBps(uint256 _t) external onlyOwner { require(_t <= 1000); rebalanceThresholdBps = _t; emit ParametersUpdated("rebalanceThresholdBps",_t); }
    function setTargetPrice(uint256 _p) external onlyOwner { targetPrice = _p; emit ParametersUpdated("targetPrice",_p); }
    function getReserves() external view returns (uint256, uint256) { return (reserve0, reserve1); }
    function getVaultTVL() external view returns (uint256) { return reserve0 + reserve1; }
    function emergencyWithdraw(address t, address to, uint256 a) external onlyOwner { require(to != address(0)); IERC20(t).sT(to, a); }
}
