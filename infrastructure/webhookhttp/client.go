package webhookhttp

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

var ErrForbiddenDestination = errors.New("forbidden webhook destination")

var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

func NewClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: validateDestination}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: timeout,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func validateDestination(_ string, address string, _ syscall.RawConn) error {
	destination, _ := netip.ParseAddrPort(address)
	if !isPublic(destination.Addr().Unmap()) {
		return fmt.Errorf("%w: %s", ErrForbiddenDestination, destination.Addr())
	}
	return nil
}

func isPublic(address netip.Addr) bool {
	if address.IsLoopback() {
		return true
	}
	return address.IsGlobalUnicast() && !address.IsPrivate() && !sharedAddressSpace.Contains(address)
}
