// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@uniswap/v3-core/contracts/libraries/TickMath.sol";
import "@uniswap/v3-core/contracts/libraries/TickBitmap.sol";
import "@uniswap/v3-core/contracts/libraries/SwapMath.sol";
import "@uniswap/v3-core/contracts/libraries/LiquidityMath.sol";

interface IERC20 {
    function transfer(address to, uint256 amount) external returns (bool);
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function balanceOf(address account) external view returns (uint256);
    function approve(address spender, uint256 amount) external returns (bool);
}

interface IERC20Metadata is IERC20 {
    function decimals() external view returns (uint8);
}

library SafeERC20 {
    function safeTransfer(IERC20 t, address to, uint256 a) internal { (bool s,) = address(t).call(abi.encodeCall(IERC20.transfer, (to, a))); require(s); }
    function safeTransferFrom(IERC20 t, address f, address to, uint256 a) internal { (bool s,) = address(t).call(abi.encodeCall(IERC20.transferFrom, (f, to, a))); require(s); }
    function forceApprove(IERC20 t, address s, uint256 v) internal { (bool s,) = address(t).call(abi.encodeCall(IERC20.approve, (s, v))); require(s); }
}

interface IUniswapV3Pool {
    function slot0() external view returns (uint160 sqrtPriceX96, int24 tick, uint16 observationCardinality, uint16 observationCardinalityNext, uint16 feeProtocol, uint8 unlocked, bool);
    function liquidity() external view returns (uint128);
    function tickSpacing() external view returns (int24);
    function tickBitmap(int16) external view returns (uint256);
    function ticks(int24) external view returns (uint128 liquidityGross, int128 liquidityNet, uint256 feeGrowthOutside0X128, uint256 feeGrowthOutside1X128, int56 tickCumulativeOutside, uint160 secondsPerLiquidityOutsideX128, uint32 secondsOutside, bool initialized);
}

interface IUniswapV3Factory { function getPool(address, address, uint24) external view returns (address); }

interface ISwapRouter {
    struct ExactInputSingleParams {
        address tokenIn;
        address tokenOut;
        uint24 fee;
        address recipient;
        uint256 deadline;
        uint256 amountIn;
        uint256 amountOutMinimum;
        uint160 sqrtPriceLimitX96;
    }
    function exactInputSingle(ExactInputSingleParams calldata params) external payable returns (uint256 amountOut);
}

contract StabilizationVault is Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    address public token0;
    address public token1;
    uint256 public reserve0;
    uint256 public reserve1;
    IUniswapV3Pool public pool;
    ISwapRouter public swapRouter;
    uint24 public fee;
    uint256 public rebalanceThresholdBps = 20;
    uint256 public targetPrice = 1;
    bool public circuitBreakerActive = false;
    address public factory;

    event Deposit(address indexed user, uint256 amount0, uint256 amount1);
    event Withdraw(address indexed user, uint256 amount0, uint256 amount1);
    event ArbitrageExecuted(uint256 amountIn, uint256 amountOut, bool success);
    event CircuitBreakerTriggered(string reason);
    event ParametersUpdated(string name, uint256 value);

    constructor(
        address _token0,
        address _token1,
        uint24 _fee,
        address _factory,
        address _swapRouter,
        address _owner
    ) Ownable(_owner) {
        token0 = _token0;
        token1 = _token1;
        factory = _factory;
        pool = IUniswapV3Pool(IUniswapV3Factory(_factory).getPool(token0, token1, _fee));
        fee = _fee;
        swapRouter = ISwapRouter(_swapRouter);
        targetPrice = 1;
    }

    function deposit(uint256 amount0, uint256 amount1) external onlyOwner nonReentrant {
        require(amount0 > 0 || amount1 > 0, "Zero amount");
        if (amount0 > 0) IERC20(token0).safeTransferFrom(msg.sender, address(this), amount0);
        if (amount1 > 0) IERC20(token1).safeTransferFrom(msg.sender, address(this), amount1);
        reserve0 += amount0;
        reserve1 += amount1;
        emit Deposit(msg.sender, amount0, amount1);
    }

    function withdraw(uint256 amount0, uint256 amount1) external onlyOwner nonReentrant {
        require(amount0 > 0 || amount1 > 0, "Zero amount");
        require(amount0 <= reserve0 && amount1 <= reserve1, "Insufficient reserves");
        reserve0 -= amount0;
        reserve1 -= amount1;
        if (amount0 > 0) IERC20(token0).safeTransfer(msg.sender, amount0);
        if (amount1 > 0) IERC20(token1).safeTransfer(msg.sender, amount1);
        emit Withdraw(msg.sender, amount0, amount1);
    }

    function getPrice() public view returns (uint160 sqrtPriceX96, int24 tick) {
        (sqrtPriceX96, tick, , , , , ) = pool.slot0();
    }

    function getLiquidity() public view returns (uint128) {
        return pool.liquidity();
    }

    /// @notice Calculate amountIn needed to reach target price, considering swap direction
    /// @param _targetPrice The target price (token1/token0) to reach
    /// @return amountIn The estimated amount of tokenIn needed
    function calculateSwapAmount(uint256 _targetPrice) public view returns (uint256 amountIn) {
        (uint160 sqrtPriceX96, , , , , , ) = pool.slot0();
        uint128 liquidity = pool.liquidity();
        require(liquidity > 0, "No liquidity");

        // Calculate current price from sqrtPriceX96
        uint256 currentPrice = (uint256(sqrtPriceX96) * uint256(sqrtPriceX96)) >> 192;

        // Determine swap direction
        // zeroForOne = true: target > current, sell token0 for token1 (buy token1)
        // zeroForOne = false: target < current, sell token1 for token0 (buy token0)
        bool zeroForOne = _targetPrice > currentPrice;

        // Convert target price to sqrtPriceX96
        uint160 targetSqrtPriceX96 = _priceToSqrtPriceX96(_targetPrice);

        // Pass direction to amountInToReachTarget
        (amountIn, , ) = amountInToReachTarget(pool, sqrtPriceX96, targetSqrtPriceX96, zeroForOne);

        // Use correct token decimals based on direction
        uint8 decimals = zeroForOne ? IERC20Metadata(token0).decimals() : IERC20Metadata(token1).decimals();
        uint256 minAmount = 10 ** decimals;
        if (amountIn < minAmount) amountIn = minAmount;
    }

    /// @notice Calculate amountIn to move from current sqrtPrice to target sqrtPrice
    /// @param poolAddr The Uniswap V3 pool
    /// @param currentSqrtPriceX96 Current sqrt price
    /// @param targetSqrtPriceX96 Target sqrt price
    /// @param zeroForOne Direction: true = token0 -> token1, false = token1 -> token0
    /// @return amountInWithFee Amount of tokenIn (including fee)
    /// @return finalTick Final tick after swap
    /// @return finalLiquidity Final liquidity after swap
    function amountInToReachTarget(
        IUniswapV3Pool poolAddr,
        uint160 currentSqrtPriceX96,
        uint160 targetSqrtPriceX96,
        bool zeroForOne
    ) public view returns (uint256 amountInWithFee, int24 finalTick, uint128 finalLiquidity) {
        (uint160 sqrtPriceX96, int24 tick, , , , , ) = poolAddr.slot0();
        uint128 liquidity = poolAddr.liquidity();
        int24 tickSpacing = poolAddr.tickSpacing();

        require(liquidity > 0);
        require(targetSqrtPriceX96 != sqrtPriceX96);

        // Override with passed current price
        sqrtPriceX96 = currentSqrtPriceX96;

        while (sqrtPriceX96 != targetSqrtPriceX96) {
            (int24 nextTick, ) = _nextInitializedTick(poolAddr, tick, tickSpacing, zeroForOne);

            if (nextTick < TickMath.MIN_TICK) nextTick = TickMath.MIN_TICK;
            if (nextTick > TickMath.MAX_TICK) nextTick = TickMath.MAX_TICK;

            uint160 sqrtPriceNextTickX96 = TickMath.getSqrtRatioAtTick(nextTick);

            uint160 stepTarget = zeroForOne
                ? (sqrtPriceNextTickX96 < targetSqrtPriceX96 ? targetSqrtPriceX96 : sqrtPriceNextTickX96)
                : (sqrtPriceNextTickX96 > targetSqrtPriceX96 ? targetSqrtPriceX96 : sqrtPriceNextTickX96);

            // Fix: Correct parameters for SwapMath.computeSwapStep
            // zeroForOne: amountRemaining = max (we want to swap until target), fee = fee, sqrtPriceLimit = 0
            // !zeroForOne: amountRemaining = max (sell token1), fee = 0, sqrtPriceLimit = MIN (don't go below)
            (uint256 amountIn, , uint160 sqrtPriceNextX96) = SwapMath.computeSwapStep(
                sqrtPriceX96,
                stepTarget,
                liquidity,
                zeroForOne ? type(int256).max : type(int256).max,
                zeroForOne ? int256(int24(fee)) : int256(0),
                zeroForOne ? uint160(0) : TickMath.MIN_SQRT_RATIO + 1
            );

            amountInWithFee += amountIn;
            sqrtPriceX96 = sqrtPriceNextX96;

            if (sqrtPriceX96 == sqrtPriceNextTickX96) {
                (, int128 liquidityNet, , , , , , ) = poolAddr.ticks(nextTick);
                liquidity = LiquidityMath.addDelta(liquidity, zeroForOne ? -liquidityNet : liquidityNet);
                tick = zeroForOne ? nextTick - 1 : nextTick;
            } else {
                tick = TickMath.getTickAtSqrtRatio(sqrtPriceX96);
            }

            if (sqrtPriceX96 == targetSqrtPriceX96) break;
        }

        finalTick = tick;
        finalLiquidity = liquidity;
    }

    function _nextInitializedTick(IUniswapV3Pool poolAddr, int24 tick, int24 ts, bool zf1) internal view returns (int24, bool) {
        int24 compressed = tick / ts;
        if (tick < 0 && tick % ts != 0) compressed--;
        return _nextInitializedTickWithinOneWord(poolAddr, compressed, ts, zf1);
    }

    function _nextInitializedTickWithinOneWord(IUniswapV3Pool poolAddr, int24 ct, int24 ts, bool lte) internal view returns (int24, bool) {
        (int16 wp, uint8 bp) = TickBitmap.position(ct);
        uint256 w = poolAddr.tickBitmap(wp);
        return TickBitmap.nextInitializedTickWithinOneWord(w, ct, ts, lte);
    }

    /// @notice Convert price (token1/token0) to sqrtPriceX96
    /// @param price The price in token1/token0 (e.g., 1 = 1:1 peg)
    /// @return The sqrt price X96
    function _priceToSqrtPriceX96(uint256 price) internal pure returns (uint160) {
        // price = sqrtPriceX96^2 / 2^192
        // sqrtPriceX96 = sqrt(price * 2^192)
        // Use 2^192 = 2^(96*2)
        uint256 priceX192 = price << 192;
        return uint160(_sqrt(priceX192));
    }

    /// @notice Simple square root function
    function _sqrt(uint256 x) internal pure returns (uint256) {
        if (x == 0) return 0;
        uint256 z = (x + 1) / 2;
        uint256 y = x;
        while (z < y) {
            y = z;
            z = (x / z + z) / 2;
        }
        return y;
    }

    function executeArbitrage() external onlyOwner nonReentrant returns (bool) {
        require(!circuitBreakerActive, "Circuit breaker active");

        (uint160 sqrtPriceX96, ) = getPrice();
        uint256 currentPrice = (uint256(sqrtPriceX96) * uint256(sqrtPriceX96)) >> 192;

        uint256 priceDiff = currentPrice > targetPrice ? currentPrice - targetPrice : targetPrice - currentPrice;
        uint256 deviationBps = (priceDiff * 10000) / targetPrice;
        require(deviationBps >= rebalanceThresholdBps, "Price deviation below threshold");

        bool zeroForOne = currentPrice > targetPrice;
        address tokenIn = zeroForOne ? token0 : token1;
        address tokenOut = zeroForOne ? token1 : token0;

        // Calculate amountIn using the multi-tick logic
        uint256 amountIn = calculateSwapAmount(targetPrice);
        require(amountIn > 0, "Invalid amount");

        uint256 balBefore = IERC20(tokenIn).balanceOf(address(this));
        require(balBefore >= amountIn, "Insufficient balance");

        IERC20(tokenIn).forceApprove(address(swapRouter), amountIn);

        uint256 amountOut = ISwapRouter(address(swapRouter)).exactInputSingle(ISwapRouter.ExactInputSingleParams({
            tokenIn: tokenIn,
            tokenOut: tokenOut,
            fee: fee,
            recipient: address(this),
            deadline: block.timestamp,
            amountIn: amountIn,
            amountOutMinimum: 0,
            sqrtPriceLimitX96: 0
        }));

        require(amountOut > 0, "No output");

        uint256 balanceInAfter = IERC20(tokenIn).balanceOf(address(this));
        uint256 balanceOutAfter = IERC20(tokenOut).balanceOf(address(this));

        if (zeroForOne) {
            reserve0 = balanceInAfter;
            reserve1 = balanceOutAfter;
        } else {
            reserve0 = balanceOutAfter;
            reserve1 = balanceInAfter;
        }

        emit ArbitrageExecuted(amountIn, amountOut, true);
        return true;
    }

    function triggerCircuitBreaker(string calldata reason) external onlyOwner {
        circuitBreakerActive = true;
        emit CircuitBreakerTriggered(reason);
    }

    function releaseCircuitBreaker() external onlyOwner {
        circuitBreakerActive = false;
    }

    function setRebalanceThresholdBps(uint256 _t) external onlyOwner {
        require(_t <= 1000);
        rebalanceThresholdBps = _t;
        emit ParametersUpdated("rebalanceThresholdBps", _t);
    }

    function setTargetPrice(uint256 _p) external onlyOwner {
        targetPrice = _p;
        emit ParametersUpdated("targetPrice", _p);
    }

    function getReserves() external view returns (uint256, uint256) {
        return (reserve0, reserve1);
    }

    function getVaultTVL() external view returns (uint256) {
        return reserve0 + reserve1;
    }

    function emergencyWithdraw(address t, address to, uint256 a) external onlyOwner {
        require(to != address(0));
        IERC20(t).safeTransfer(to, a);
    }
}