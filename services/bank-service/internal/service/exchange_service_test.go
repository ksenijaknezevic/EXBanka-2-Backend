package service_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
)

// ─── Mocks ────────────────────────────────────────────────────────────────────

type mockExchangeProvider struct {
	rates map[string]float64
	err   error
}

func (m *mockExchangeProvider) GetMidRates(_ context.Context) (map[string]float64, error) {
	return m.rates, m.err
}

type mockExchangeTransferRepo struct {
	result *domain.ExchangeTransferResult
	err    error
}

func (m *mockExchangeTransferRepo) ExecuteTransfer(
	_ context.Context,
	input domain.ExchangeTransferInput,
	conversion domain.ExchangeConversionResult,
) (*domain.ExchangeTransferResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	// Default: echo back computed values from input + conversion.
	return &domain.ExchangeTransferResult{
		ReferenceID:     "KNV-test-001",
		SourceAccountID: input.SourceAccountID,
		TargetAccountID: input.TargetAccountID,
		FromOznaka:      input.FromOznaka,
		ToOznaka:        input.ToOznaka,
		OriginalAmount:  input.Amount,
		GrossAmount:     conversion.Bruto,
		Provizija:       conversion.Provizija,
		NetAmount:       conversion.Result,
		ViaRSD:          conversion.ViaRSD,
		RateNote:        conversion.RateNote,
	}, nil
}

// testRates are deterministic mid rates (RSD per 1 unit) used across all tests.
var testRates = map[string]float64{
	"EUR": 117.00,
	"USD": 107.75,
	"GBP": 136.75,
}

func newTestService() domain.ExchangeService {
	return service.NewExchangeService(
		&mockExchangeProvider{rates: testRates},
		&mockExchangeTransferRepo{},
	)
}

func newTestServiceWithRepoErr(repoErr error) domain.ExchangeService {
	return service.NewExchangeService(
		&mockExchangeProvider{rates: testRates},
		&mockExchangeTransferRepo{err: repoErr},
	)
}

// approxEqual returns true when a and b differ by less than tolerance.
func approxEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

// ─── CalculateExchange tests ─────────────────────────────────────────────────

func TestCalculateExchange_SameCurrency(t *testing.T) {
	svc := newTestService()
	result, err := svc.CalculateExchange(context.Background(), "EUR", "EUR", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result != 100 {
		t.Errorf("same-currency: expected result=100, got %.4f", result.Result)
	}
	if result.Provizija != 0 {
		t.Errorf("same-currency: expected provizija=0, got %.4f", result.Provizija)
	}
	if result.ViaRSD {
		t.Error("same-currency: expected ViaRSD=false")
	}
}

func TestCalculateExchange_InvalidAmount_Zero(t *testing.T) {
	svc := newTestService()
	_, err := svc.CalculateExchange(context.Background(), "EUR", "RSD", 0)
	if !errors.Is(err, domain.ErrExchangeInvalidAmount) {
		t.Errorf("expected ErrExchangeInvalidAmount, got %v", err)
	}
}

func TestCalculateExchange_InvalidAmount_Negative(t *testing.T) {
	svc := newTestService()
	_, err := svc.CalculateExchange(context.Background(), "EUR", "RSD", -50)
	if !errors.Is(err, domain.ErrExchangeInvalidAmount) {
		t.Errorf("expected ErrExchangeInvalidAmount, got %v", err)
	}
}

func TestCalculateExchange_ForeignToRSD(t *testing.T) {
	// 100 EUR → RSD
	// kupovni = 117.00 * (1 - 0.005) = 116.415
	// bruto   = 100 * 116.415 = 11641.5
	// prov    = 11641.5 * 0.005 = 58.2075
	// result  = 11641.5 - 58.2075 = 11583.2925
	svc := newTestService()
	result, err := svc.CalculateExchange(context.Background(), "EUR", "RSD", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ViaRSD {
		t.Error("EUR→RSD: expected ViaRSD=false")
	}

	expectedBruto := 100.0 * (117.00 * 0.995)
	if !approxEqual(result.Bruto, expectedBruto, 0.01) {
		t.Errorf("EUR→RSD: bruto want %.4f, got %.4f", expectedBruto, result.Bruto)
	}
	expectedResult := expectedBruto * 0.995
	if !approxEqual(result.Result, expectedResult, 0.01) {
		t.Errorf("EUR→RSD: result want %.4f, got %.4f", expectedResult, result.Result)
	}
}

func TestCalculateExchange_RSDToForeign(t *testing.T) {
	// 1000 RSD → EUR
	// prodajni = 117.00 * (1 + 0.005) = 117.585
	// bruto    = 1000 / 117.585 ≈ 8.5051
	// prov     = bruto * 0.005
	// result   = bruto - prov
	svc := newTestService()
	result, err := svc.CalculateExchange(context.Background(), "RSD", "EUR", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ViaRSD {
		t.Error("RSD→EUR: expected ViaRSD=false")
	}
	if result.Bruto <= 0 || result.Result <= 0 {
		t.Error("RSD→EUR: expected positive bruto and result")
	}

	expectedBruto := 1000.0 / (117.00 * 1.005)
	if !approxEqual(result.Bruto, expectedBruto, 0.001) {
		t.Errorf("RSD→EUR: bruto want %.6f, got %.6f", expectedBruto, result.Bruto)
	}
	// Commission is 0.5% of bruto.
	if !approxEqual(result.Provizija, result.Bruto*0.005, 0.0001) {
		t.Errorf("RSD→EUR: provizija should be 0.5%% of bruto")
	}
}

func TestCalculateExchange_CrossCurrency_ViaRSD(t *testing.T) {
	// EUR → USD: must go via RSD, ViaRSD=true
	svc := newTestService()
	result, err := svc.CalculateExchange(context.Background(), "EUR", "USD", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ViaRSD {
		t.Error("EUR→USD: expected ViaRSD=true (cross-currency)")
	}
	if result.Result <= 0 {
		t.Error("EUR→USD: expected positive result")
	}
	// EUR kupovni ≈ 116.415; USD prodajni ≈ 108.2875
	// rsd = 100 * 116.415 = 11641.5; bruto = 11641.5 / 108.2875 ≈ 107.5
	expectedRSD := 100.0 * (117.00 * 0.995)
	expectedBruto := expectedRSD / (107.75 * 1.005)
	if !approxEqual(result.Bruto, expectedBruto, 0.1) {
		t.Errorf("EUR→USD: bruto want ≈%.4f, got %.4f", expectedBruto, result.Bruto)
	}
}

func TestCalculateExchange_UnknownCurrency(t *testing.T) {
	svc := newTestService()
	_, err := svc.CalculateExchange(context.Background(), "EUR", "XYZ", 100)
	if err == nil {
		t.Error("expected error for unsupported currency XYZ")
	}
}

func TestCommissionRate_IsHalfPercent(t *testing.T) {
	svc := newTestService()
	result, err := svc.CalculateExchange(context.Background(), "EUR", "RSD", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pct := result.Provizija / result.Bruto
	if !approxEqual(pct, 0.005, 0.0001) {
		t.Errorf("provizija should be 0.5%% of bruto, got %.4f%%", pct*100)
	}
	if !approxEqual(result.Result, result.Bruto-result.Provizija, 0.001) {
		t.Errorf("result should equal bruto - provizija")
	}
}

// ─── ExecuteExchangeTransfer tests ───────────────────────────────────────────

func TestExecuteExchangeTransfer_SameCurrency_Rejected(t *testing.T) {
	svc := newTestService()
	_, err := svc.ExecuteExchangeTransfer(context.Background(), domain.ExchangeTransferInput{
		VlasnikID: 1, SourceAccountID: 10, TargetAccountID: 20,
		FromOznaka: "EUR", ToOznaka: "EUR", Amount: 100,
	})
	if !errors.Is(err, domain.ErrExchangeSameCurrency) {
		t.Errorf("expected ErrExchangeSameCurrency, got %v", err)
	}
}

func TestExecuteExchangeTransfer_SameAccount_Rejected(t *testing.T) {
	svc := newTestService()
	_, err := svc.ExecuteExchangeTransfer(context.Background(), domain.ExchangeTransferInput{
		VlasnikID: 1, SourceAccountID: 10, TargetAccountID: 10,
		FromOznaka: "EUR", ToOznaka: "RSD", Amount: 100,
	})
	if !errors.Is(err, domain.ErrExchangeSameAccount) {
		t.Errorf("expected ErrExchangeSameAccount, got %v", err)
	}
}

func TestExecuteExchangeTransfer_InvalidAmount_Rejected(t *testing.T) {
	svc := newTestService()
	for _, amount := range []float64{0, -1, -0.001} {
		_, err := svc.ExecuteExchangeTransfer(context.Background(), domain.ExchangeTransferInput{
			VlasnikID: 1, SourceAccountID: 10, TargetAccountID: 20,
			FromOznaka: "EUR", ToOznaka: "RSD", Amount: amount,
		})
		if !errors.Is(err, domain.ErrExchangeInvalidAmount) {
			t.Errorf("amount=%.4f: expected ErrExchangeInvalidAmount, got %v", amount, err)
		}
	}
}

func TestExecuteExchangeTransfer_HappyPath(t *testing.T) {
	svc := newTestService()
	result, err := svc.ExecuteExchangeTransfer(context.Background(), domain.ExchangeTransferInput{
		VlasnikID: 1, SourceAccountID: 10, TargetAccountID: 20,
		FromOznaka: "EUR", ToOznaka: "RSD", Amount: 100,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.FromOznaka != "EUR" || result.ToOznaka != "RSD" {
		t.Errorf("currency mismatch: from=%s to=%s", result.FromOznaka, result.ToOznaka)
	}
	if result.NetAmount <= 0 {
		t.Error("expected positive net amount")
	}
	if result.OriginalAmount != 100 {
		t.Errorf("expected OriginalAmount=100, got %.4f", result.OriginalAmount)
	}
}

func TestExecuteExchangeTransfer_ViaRSD_CrossCurrency(t *testing.T) {
	svc := newTestService()
	result, err := svc.ExecuteExchangeTransfer(context.Background(), domain.ExchangeTransferInput{
		VlasnikID: 1, SourceAccountID: 10, TargetAccountID: 20,
		FromOznaka: "EUR", ToOznaka: "USD", Amount: 50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ViaRSD {
		t.Error("EUR→USD: expected ViaRSD=true in result")
	}
}

func TestExecuteExchangeTransfer_InsufficientFunds_PropagatedFromRepo(t *testing.T) {
	svc := newTestServiceWithRepoErr(domain.ErrExchangeInsufficientFunds)
	_, err := svc.ExecuteExchangeTransfer(context.Background(), domain.ExchangeTransferInput{
		VlasnikID: 1, SourceAccountID: 10, TargetAccountID: 20,
		FromOznaka: "EUR", ToOznaka: "RSD", Amount: 999999,
	})
	if !errors.Is(err, domain.ErrExchangeInsufficientFunds) {
		t.Errorf("expected ErrExchangeInsufficientFunds from repo, got %v", err)
	}
}

func TestExecuteExchangeTransfer_WrongCurrency_PropagatedFromRepo(t *testing.T) {
	svc := newTestServiceWithRepoErr(domain.ErrExchangeWrongCurrency)
	_, err := svc.ExecuteExchangeTransfer(context.Background(), domain.ExchangeTransferInput{
		VlasnikID: 1, SourceAccountID: 10, TargetAccountID: 20,
		FromOznaka: "EUR", ToOznaka: "RSD", Amount: 100,
	})
	if !errors.Is(err, domain.ErrExchangeWrongCurrency) {
		t.Errorf("expected ErrExchangeWrongCurrency from repo, got %v", err)
	}
}
