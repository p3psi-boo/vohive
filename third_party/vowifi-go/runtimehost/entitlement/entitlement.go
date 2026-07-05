// Package entitlement 提供 E911 运行时 HTTP 集成。
package entitlement

import (
	"context"
	"fmt"

	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/providers/att"
	"github.com/iniwex5/vowifi-go/runtimehost/e911"
)

type Client struct {
	httpClient e911.HTTPClient
}

func NewClient(client e911.HTTPClient) *Client {
	return &Client{httpClient: client}
}

func (c *Client) StartUpdate(ctx context.Context, identity e911.Identity, mcc, mnc string) (*e911.HTTPResponse, error) {
	switch {
	case mcc == "310" && (mnc == "280" || mnc == "150" || mnc == "170" || mnc == "410"):
		provider := att.New(mcc, mnc)
		return provider.StartAddressUpdate(ctx, identity, c.httpClient)
	default:
		return nil, fmt.Errorf("entitlement: unsupported carrier MCC=%s MNC=%s", mcc, mnc)
	}
}
