package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-resty/resty/v2"
)

const (
	checkInterval       = 12 * time.Hour
	thresholdWETH       = 0.002
	delayBetweenChecks = 1 * time.Second // Delay 1 detik antara pengecekan alamat
)

// Struktur untuk menyimpan konfigurasi dari settings.json
type Settings struct {
	BASERPCURL           string `json:"base_rpc_url"`
	TelegramBotToken     string `json:"telegram_bot_token"`
	TelegramChatID       int64  `json:"telegram_chat_id"`
	WETHContractAddress  string `json:"weth_contract_address"`
	AddressesFile        string `json:"addresses_file"`
}

// ABI WBNB
const wbnbABI = `[{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"src","type":"address"},{"indexed":true,"internalType":"address","name":"guy","type":"address"},{"indexed":false,"internalType":"uint256","name":"wad","type":"uint256"}],"name":"Approval","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"dst","type":"address"},{"indexed":false,"internalType":"uint256","name":"wad","type":"uint256"}],"name":"Deposit","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"src","type":"address"},{"indexed":true,"internalType":"address","name":"dst","type":"address"},{"indexed":false,"internalType":"uint256","name":"wad","type":"uint256"}],"name":"Transfer","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"src","type":"address"},{"indexed":false,"internalType":"uint256","name":"wad","type":"uint256"}],"name":"Withdrawal","type":"event"},{"payable":true,"stateMutability":"payable","type":"fallback"},{"constant":true,"inputs":[{"internalType":"address","name":"","type":"address"},{"internalType":"address","name":"","type":"address"}],"name":"allowance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"guy","type":"address"},{"internalType":"uint256","name":"wad","type":"uint256"}],"name":"approve","outputs":[{"internalType":"bool","name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[],"name":"deposit","outputs":[],"payable":true,"stateMutability":"payable","type":"function"},{"constant":true,"inputs":[],"name":"name","outputs":[{"internalType":"string","name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"totalSupply","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"dst","type":"address"},{"internalType":"uint256","name":"wad","type":"uint256"}],"name":"transfer","outputs":[{"internalType":"bool","name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"src","type":"address"},{"internalType":"address","name":"dst","type":"address"},{"internalType":"uint256","name":"wad","type":"uint256"}],"name":"transferFrom","outputs":[{"internalType":"bool","name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":false,"inputs":[{"internalType":"uint256","name":"wad","type":"uint256"}],"name":"withdraw","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"}]`

// Mengirim pesan ke Telegram
func sendTelegramMessage(botToken string, chatID int64, message string) {
	client := resty.New()
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	resp, err := client.R().
		SetQueryParam("chat_id", fmt.Sprintf("%d", chatID)).
		SetQueryParam("text", message).
		Get(url)

	if err != nil {
		log.Printf("Gagal mengirim pesan ke Telegram: %v", err)
	} else if resp.StatusCode() != 200 {
		log.Printf("Gagal mengirim pesan ke Telegram, status code: %d, body: %s", resp.StatusCode(), resp.String())
	} else {
		log.Println("Pesan berhasil dikirim ke Telegram")
	}
}

// Membaca alamat dari file
func readAddressesFromFile(filename string) ([]string, error) {
	var addresses []string
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) > 0 {
			addresses = append(addresses, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return addresses, nil
}

// Memeriksa saldo dari setiap alamat
func checkBalances(client *ethclient.Client, contractAddress common.Address, addresses []string, settings Settings) {
	parsedABI, err := abi.JSON(strings.NewReader(wbnbABI))
	if err != nil {
		log.Fatalf("Gagal parsing ABI: %v", err)
	}

	for i, addr := range addresses {
		address := common.HexToAddress(addr)
		log.Printf("Memeriksa alamat %d dari %d: %s", i+1, len(addresses), addr)

		callData, err := parsedABI.Pack("balanceOf", address)
		if err != nil {
			log.Printf("Gagal mem-packing data untuk alamat %s: %v", addr, err)
			continue
		}

		callMsg := ethereum.CallMsg{
			To:   &contractAddress,
			Data: callData,
		}

		res, err := client.CallContract(context.Background(), callMsg, nil)
		if err != nil {
			log.Printf("Gagal memeriksa saldo untuk alamat %s: %v", addr, err)
			continue
		}

		// Debug log to show result data
		log.Printf("Hasil raw dari kontrak untuk alamat %s: %x", addr, res)

		// Unpacking result dengan tipe data yang benar
		var balance *big.Int
		err = parsedABI.UnpackIntoInterface(&balance, "balanceOf", res)
		if err != nil {
			log.Printf("Gagal unpack result untuk alamat %s: %v", addr, err)
			continue
		}

		balanceFloat := new(big.Float).SetInt(balance)
		balanceWETH := new(big.Float).Quo(balanceFloat, big.NewFloat(1e18)) // Konversi wei ke WETH

		log.Printf("Alamat: %s, Saldo WETH: %s WETH", addr, balanceWBNB.String())

		if balanceWETH.Cmp(big.NewFloat(thresholdWETH)) > 0 {
			message := fmt.Sprintf("Alamat: %s, Saldo WETH: %s WETH", addr, balanceWETH.String())
			sendTelegramMessage(settings.TelegramBotToken, settings.TelegramChatID, message)
		}

		time.Sleep(delayBetweenChecks) // Delay 1 detik antara pengecekan alamat
	}
}

func main() {
	for {
		file, err := ioutil.ReadFile("settings.json")
		if err != nil {
			log.Fatalf("Gagal membaca file settings.json: %v", err)
		}

		var settings Settings
		if err := json.Unmarshal(file, &settings); err != nil {
			log.Fatalf("Gagal mem-parsing settings.json: %v", err)
		}

		addresses, err := readAddressesFromFile(settings.AddressesFile)
		if err != nil {
			log.Fatalf("Gagal membaca file alamat: %v", err)
		}

		client, err := ethclient.Dial(settings.BSCRPCURL)
		if err != nil {
			log.Fatalf("Gagal terhubung ke BASE RPC: %v", err)
		}

		contractAddress := common.HexToAddress(settings.WETHContractAddress)

		checkBalances(client, contractAddress, addresses, settings)

		log.Println("Menunggu 12 jam sebelum memeriksa saldo lagi...")
		time.Sleep(checkInterval)
	}
}
