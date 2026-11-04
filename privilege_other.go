//go:build !linux

package main

import (
	"errors"
	"net"
)

func unixPeerUID(net.Conn) (uint32, error) {
	return 0, errors.New("Unix peer credentials require Linux")
}
func prepareBrokerActivation() error { return errors.New("privilege separation requires Linux") }
func runPrivilegeBroker(*App) error  { return errors.New("privilege separation requires Linux") }
func runUnprivilegedWeb() error      { return errors.New("privilege separation requires Linux") }
