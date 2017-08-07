package lprlib

import (
	"fmt"
	"testing"
)

func TestDNS(t *testing.T) {
	fmt.Println("---Start TestDNS---")
	defer fmt.Println("--- End Test---")

	var lpr LprSend

	ip, err := lpr.GetIP("hq.documatrix.com")
	if err != nil {
		fmt.Println(err.Error())
		t.Fail()
	} else {
		fmt.Println(ip.IP, err)
	}
}
