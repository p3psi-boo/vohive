package e911

import (
	"context"
	"errors"
	"net/http"
)

var (
	ErrUnsupportedProvider      = errors.New("unsupported carrier for entitlement")
	ErrChallengeNotImplemented  = errors.New("entitlement challenge flow not implemented")
	ErrWebsheetUnavailable      = errors.New("carrier websheet unavailable")
)

type HeaderPair struct {
	Key   string
	Value string
}

type Identity struct {
	IMSI        string
	IMEI        string
	MCC         string
	MNC         string
	SIPUsername string
	DisplayName string
}

type Request struct {
	Carrier     interface{}
	Identity    Identity
	AKAProvider interface{}
	Client      HTTPClient
	Trace       interface{}
}

type HTTPClient interface {
	Do(req *HTTPRequest) (*HTTPResponse, error)
}

type HTTPRequest struct {
	Method  string
	URL     string
	Headers []HeaderPair
	Body    []byte
}

type HTTPResponse struct {
	StatusCode  int
	Body        []byte
	URL         string
	UserData    string
	ContentType string
	Title       string
}

func StartEmergencyAddressUpdate(ctx context.Context, req Request) (*HTTPResponse, error) {
	return nil, ErrUnsupportedProvider
}

func NewDefaultHTTPClient() HTTPClient {
	return &defaultHTTPClient{client: http.DefaultClient}
}

type defaultHTTPClient struct {
	client *http.Client
}

func (c *defaultHTTPClient) Do(req *HTTPRequest) (*HTTPResponse, error) {
	return nil, errors.New("not implemented")
}
