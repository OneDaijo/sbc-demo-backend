package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	b "github.com/stellar/go/build"
	"github.com/stellar/go/clients/horizon"
	"github.com/stellar/go/keypair"
)

func sendTransaction(tx *b.TransactionBuilder, from *string) error {

	txe, err := tx.Sign(*from)
	if err != nil {
		return err
	}

	txeB64, err := txe.Base64()

	if err != nil {
		return err
	}

	resp, err := horizon.DefaultTestNetClient.SubmitTransaction(fmt.Sprintf("%s", txeB64))
	if err != nil {
		fmt.Println(err)
		herr, isHorizonError := err.(*horizon.Error)
		if isHorizonError {
			resultCodes, err := herr.ResultCodes()
			if err != nil {
				fmt.Println("failed to extract result codes from horizon response")
				return err
			}
			fmt.Println(resultCodes)
		}
		return err
	}

	fmt.Println("transaction posted in ledger:", resp.Ledger)

	return nil

}

func main() {
	pair, err := keypair.Random()
	if err != nil {
		log.Fatal(err)
	}

	log.Println(pair.Seed())
	log.Println(pair.Address())

	seed_bytes, err := ioutil.ReadFile("../server/stellar_seed.txt")
	if err != nil {
		panic(err)
	}
	seed_string := string(seed_bytes)

	to, err := keypair.Parse(seed_string)
	if err != nil {
		panic(err)
	}
	// pair is the pair that was generated from previous example, or create a pair based on
	// existing keys.
	address := pair.Address()
	resp, err := http.Get("https://horizon-testnet.stellar.org/friendbot?addr=" + address)
	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(body))

	tx, err := b.Transaction(
		b.SourceAccount{AddressOrSeed: pair.Address()},
		b.TestNetwork,
		b.AutoSequence{SequenceProvider: horizon.DefaultTestNetClient},
		b.Payment(
			b.Destination{AddressOrSeed: to.Address()},
			b.NativeAmount{Amount: "9990.0"},
		),
	)

	if err != nil {
		panic(err)
	}
	signing_seed := pair.Seed()

	err = sendTransaction(tx, &signing_seed)

	if err != nil {
		panic(err)
	}

	account, err := horizon.DefaultTestNetClient.LoadAccount(to.Address())
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Balances for account:", to.Address())

	for _, balance := range account.Balances {
		log.Println(balance)
	}
}
