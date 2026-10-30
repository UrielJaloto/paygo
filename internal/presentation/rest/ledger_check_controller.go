package rest

import (
	"context"
	"net/http"

	"github.com/UrielJaloto/paygo/internal/services/ledgercheck"
	"github.com/gin-gonic/gin"
)

type LedgerChecker interface {
	Check(ctx context.Context) (ledgercheck.CheckOutput, error)
}

type LedgerCheckController struct {
	checker LedgerChecker
}

type ledgerCheckResponse struct {
	OK                   bool     `json:"ok"`
	Accounts             int      `json:"accounts,omitempty"`
	Mismatches           []string `json:"mismatches,omitempty"`
	UnbalancedCurrencies []string `json:"unbalanced_currencies,omitempty"`
}

func NewLedgerCheckController(checker LedgerChecker) *LedgerCheckController {
	return &LedgerCheckController{checker: checker}
}

func (l *LedgerCheckController) Check(c *gin.Context) {
	output, err := l.checker.Check(c.Request.Context())
	if err != nil {
		PresentError(c, err)
		return
	}
	status := http.StatusOK
	if !output.OK {
		status = http.StatusConflict
	}
	c.JSON(status, ledgerCheckResponse(output))
}

func registerLedgerCheckRoutes(group *gin.RouterGroup, controller *LedgerCheckController) {
	group.GET("/ledger/check", controller.Check)
}
