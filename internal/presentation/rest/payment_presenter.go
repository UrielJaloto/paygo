package rest

import (
	"net/http"

	paymentservice "github.com/UrielJaloto/paygo/internal/services/payment"
	"github.com/gin-gonic/gin"
)

type chargeResponse struct {
	ID             string `json:"id"`
	AccountID      string `json:"account_id"`
	Status         string `json:"status"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
	PixCopyPaste   string `json:"pix_copy_paste"`
	PixQRPNGBase64 string `json:"pix_qr_png_base64"`
	ExpiresAt      string `json:"expires_at"`
	CreatedAt      string `json:"created_at"`
}

type payoutResponse struct {
	ID         string `json:"id"`
	AccountID  string `json:"account_id"`
	Status     string `json:"status"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	PixKeyType string `json:"pix_key_type"`
	CreatedAt  string `json:"created_at"`
}

func toChargeResponse(output paymentservice.ChargeOutput) chargeResponse {
	return chargeResponse{
		ID:             output.ID,
		AccountID:      output.AccountID,
		Status:         output.Status,
		Amount:         formatAmount(output.AmountMinor, output.Currency),
		Currency:       output.Currency,
		PixCopyPaste:   output.PixCopyPaste,
		PixQRPNGBase64: output.PixQRBase64,
		ExpiresAt:      formatTimestamp(output.ExpiresAt),
		CreatedAt:      formatTimestamp(output.CreatedAt),
	}
}

func toPayoutResponse(output paymentservice.PayoutOutput) payoutResponse {
	return payoutResponse{
		ID:         output.ID,
		AccountID:  output.AccountID,
		Status:     output.Status,
		Amount:     formatAmount(output.AmountMinor, output.Currency),
		Currency:   output.Currency,
		PixKeyType: output.PixKeyType,
		CreatedAt:  formatTimestamp(output.CreatedAt),
	}
}

func presentCreatedCharge(ginContext *gin.Context, output paymentservice.ChargeOutput, err error) {
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	status := http.StatusCreated
	if output.IsReplay {
		status = markReplayed(ginContext)
	}
	ginContext.JSON(status, toChargeResponse(output))
}

func presentCreatedPayout(ginContext *gin.Context, output paymentservice.PayoutOutput, err error) {
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	status := http.StatusCreated
	if output.IsReplay {
		status = markReplayed(ginContext)
	}
	ginContext.JSON(status, toPayoutResponse(output))
}

func markReplayed(ginContext *gin.Context) int {
	ginContext.Header(idempotentReplayedHeader, idempotentReplayedPresent)
	return http.StatusOK
}
