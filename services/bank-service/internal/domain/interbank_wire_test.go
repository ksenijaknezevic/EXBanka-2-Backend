package domain_test

// Verifikacija wire-formata sa Bankom 2 (RN-222): Asset i TxAccount moraju da se
// serijalizuju tačno kao tagged union sa diskriminatorom `type` i payload-om pod
// ključem `asset` (za Asset). Ovi testovi su ugovor protiv kog Banka 2 deserijalizuje.

import (
	"encoding/json"
	"testing"

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
