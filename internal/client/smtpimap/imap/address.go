package imap

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
)

func GetAddressName(address imap.Address) string {
	return fmt.Sprintf("%s (%s)", address.Name, address.Addr())
}

func GetAddressNames(addresses []imap.Address) []string {
	var addrs []string = make([]string, len(addresses))
	for i, addr := range addresses {
		addrs[i] = GetAddressName(addr)
	}
	return addrs
}
