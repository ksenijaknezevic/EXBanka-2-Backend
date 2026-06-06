package domain_test

// Verifikacija wire-formata sa Bankom 2 (RN-222): Asset i TxAccount moraju da se
// serijalizuju tačno kao tagged union sa diskriminatorom `type` i payload-om pod
// ključem `asset` (za Asset). Ovi testovi su ugovor protiv kog Banka 2 deserijalizuje.

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
)

func TestAsset_JSON_MatchesBanka2Shape(t *testing.T) {
	monas := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	b, err := json.Marshal(monas)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"MONAS","asset":{"currency":"USD"}}`, string(b))

	stock := domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: "AAPL"}}
	b, err = json.Marshal(stock)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"STOCK","asset":{"ticker":"AAPL"}}`, string(b))

	// Round-trip: payload pod `asset`, ne flat.
	var back domain.Asset
	require.NoError(t, json.Unmarshal([]byte(`{"type":"MONAS","asset":{"currency":"RSD"}}`), &back))
	assert.Equal(t, domain.AssetTypeMonas, back.Type)
	require.NotNil(t, back.MonAs)
	assert.Equal(t, "RSD", back.MonAs.Currency)
}

func TestTxAccount_JSON_MatchesBanka2Shape(t *testing.T) {
	person := domain.TxAccount{Type: domain.AccountKindPerson, ID: &domain.ForeignBankId{RoutingNumber: 222, ID: "C-12"}}
	b, err := json.Marshal(person)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"PERSON","id":{"routingNumber":222,"id":"C-12"}}`, string(b))

	num := "222000000000000123"
	account := domain.TxAccount{Type: domain.AccountKindAccount, Num: &num}
	b, err = json.Marshal(account)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"ACCOUNT","num":"222000000000000123"}`, string(b))
}

// Regresija: Banka 2 šalje OTC accept premium NEW_TX gde OPTION asset nosi
// decimalnu količinu ("amount":4.0000). Ranije je OptionDescription.Amount bio
// int32 → ceo NEW_TX nije mogao da se dekodira → vraćali smo 400 → premija nije
// naplaćena kupcu. Ovaj test čuva da decimalni amount prolazi dekodiranje.
func TestTransaction_DecodesOptionPremiumNewTx_DecimalAmount(t *testing.T) {
	// Stvarni oblik premium NEW_TX-a (AMZN, 4 kom) koji nam Banka 2 šalje.
	payload := `{
      "postings":[
        {"account":{"type":"PERSON","id":{"routingNumber":265,"id":"4"}},"amount":-17.0000,"asset":{"type":"MONAS","asset":{"currency":"USD"}}},
        {"account":{"type":"ACCOUNT","num":"222000131234567812"},"amount":17.0000,"asset":{"type":"MONAS","asset":{"currency":"USD"}}},
        {"account":{"type":"PERSON","id":{"routingNumber":265,"id":"4"}},"amount":1,"asset":{"type":"OPTION","asset":{"negotiationId":{"routingNumber":222,"id":"5bbe7c16"},"stock":{"ticker":"AMZN"},"pricePerUnit":{"currency":"USD","amount":12.0000},"settlementDate":"2026-06-08T00:00:00Z","amount":4.0000}}},
        {"account":{"type":"PERSON","id":{"routingNumber":222,"id":"C-2"}},"amount":-1,"asset":{"type":"OPTION","asset":{"negotiationId":{"routingNumber":222,"id":"5bbe7c16"},"stock":{"ticker":"AMZN"},"pricePerUnit":{"currency":"USD","amount":12.0000},"settlementDate":"2026-06-08T00:00:00Z","amount":4.0000}}}
      ],
      "transactionId":{"routingNumber":222,"id":"f16f2fa3"},
      "message":"OTC accept","paymentCode":"OTC","paymentPurpose":"Premium za opcioni ugovor"
    }`

	var tx domain.Transaction
	require.NoError(t, json.Unmarshal([]byte(payload), &tx), "decimalni OPTION amount mora da se dekodira")
	require.Len(t, tx.Postings, 4)

	// Pronađi OPTION posting i potvrdi da je količina očuvana kao 4.
	var found bool
	for _, p := range tx.Postings {
		if p.Asset.Type == domain.AssetTypeOption {
			require.NotNil(t, p.Asset.Option)
			assert.True(t, p.Asset.Option.Amount.Equal(decimal.NewFromInt(4)),
				"OPTION amount mora biti 4, dobijeno %s", p.Asset.Option.Amount)
			found = true
		}
	}
	assert.True(t, found, "očekivan bar jedan OPTION posting")
}
