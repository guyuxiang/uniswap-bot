package executor

import (
	"context"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var ERC20ABI = `[{"constant":true,"inputs":[],"name":"name","outputs":[{"name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"_spender","type":"address"},{"name":"_value","type":"uint256"}],"name":"approve","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"totalSupply","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"_from","type":"address"},{"name":"_value","type":"uint256"}],"name":"transferFrom","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"name":"_owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"balance","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"_to","type":"address"},{"name":"_value","type":"uint256"}],"name":"transfer","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"name":"_owner","type":"address"},{"name":"_spender","type":"address"}],"name":"allowance","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"}]`

type ERC20 struct {
	abi  abi.ABI
	addr common.Address
	client *ethclient.Client
}

func NewERC20(address common.Address, client *ethclient.Client) (*ERC20, error) {
	parsed, err := abi.JSON(strings.NewReader(ERC20ABI))
	if err != nil {
		return nil, err
	}

	return &ERC20{
		abi:  parsed,
		addr: address,
		client: client,
	}, nil
}

func (e *ERC20) BalanceOf(ctx context.Context, owner common.Address) (*big.Int, error) {
	data, err := e.abi.Pack("balanceOf", owner)
	if err != nil {
		return nil, err
	}

	msg := ethereum.CallMsg{
		To:   &e.addr,
		Data: data,
	}

	result, err := e.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, err
	}

	balance := new(big.Int).SetBytes(result)
	return balance, nil
}

func (e *ERC20) Allowance(ctx context.Context, owner, spender common.Address) (*big.Int, error) {
	data, err := e.abi.Pack("allowance", owner, spender)
	if err != nil {
		return nil, err
	}

	msg := ethereum.CallMsg{
		To:   &e.addr,
		Data: data,
	}

	result, err := e.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, err
	}

	allowance := new(big.Int).SetBytes(result)
	return allowance, nil
}

func GetTokenBalance(ctx context.Context, client *ethclient.Client, tokenAddr, ownerAddr common.Address) (*big.Int, error) {
	if tokenAddr == common.HexToAddress("0x0000000000000000000000000000000000000000") {
		return client.BalanceAt(ctx, ownerAddr, nil)
	}

	erc20, err := NewERC20(tokenAddr, client)
	if err != nil {
		return big.NewInt(0), err
	}

	return erc20.BalanceOf(ctx, ownerAddr)
}
