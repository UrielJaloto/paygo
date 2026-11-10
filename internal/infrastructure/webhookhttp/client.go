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

const (
	maximumIdleConnections = 100
	idleConnectionTimeout  = 90 * time.Second
)

var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

func NewClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: validateDestination}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: timeout,
		MaxIdleConns:        maximumIdleConnections,
		IdleConnTimeout:     idleConnectionTimeout,
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
	destination, err := netip.ParseAddrPort(address)
	if err != nil || !isPublic(destination.Addr().Unmap()) {
		return errors.Join(fmt.Errorf("%w: %s", ErrForbiddenDestination, address), err)
	}
	return nil
}

func isPublic(address netip.Addr) bool {
	if address.IsLoopback() {
		return true
	}
	return address.IsGlobalUnicast() && !address.IsPrivate() && !sharedAddressSpace.Contains(address)
}
