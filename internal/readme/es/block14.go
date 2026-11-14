// Extracted from README.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package es

import (
	"fmt"
	"github.com/flashpi1/flashpi-go"
	"net/http"
	"net/url"
)

// Block14 is go block 14 of README.md.
func Block14() error {
	proxy, err := url.Parse("http://proxy.interno:3128")
	if err != nil {
		return err
	}
	client, err := flashpi.NewClient(flashpi.WithHTTPClient(&http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxy)},
	}))
	if err != nil {
		return err
	}
	fmt.Println(client.Environment())
	return nil
}
