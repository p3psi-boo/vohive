// Package att 提供 AT&T 特定 E911 Entitlement 实现。
package att

import (
	"context"
	"fmt"

	"github.com/iniwex5/vowifi-go/runtimehost/e911"
)

type Provider struct {
	mcc string
	mnc string
}

func New(mcc, mnc string) *Provider {
	return &Provider{mcc: mcc, mnc: mnc}
}

func (p *Provider) Type() string { return "att" }

func (p *Provider) StartAddressUpdate(ctx context.Context, identity e911.Identity, client e911.HTTPClient) (*e911.HTTPResponse, error) {
	req := &e911.HTTPRequest{
		Method: "POST",
		URL:    "https://sentitlement2.mobile.att.net/entitlement/v3/ws/registration",
		Headers: []e911.HeaderPair{
			{Key: "Content-Type", Value: "application/json"},
			{Key: "X-Protocol-Version", Value: "3"},
			{Key: "X-3GPP-IMEI", Value: identity.IMEI},
			{Key: "X-3GPP-IMSI", Value: identity.IMSI},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("att entitlement: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("att entitlement: HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (p *Provider) BuildWebsheetURL(host string) string {
	return fmt.Sprintf("https://%s/websheet/v1/emergency", host)
}
