package orderly

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

type Client interface {
	// GetAccount checks whether a wallet is registered with the given broker.
	// https://orderly.network/docs/build-on-omnichain/evm-api/restful-api/public/check-if-wallet-is-registered
	GetAccount(ctx context.Context, address, brokerID string) (*GetAccountResponse, error)

	// GetNonce fetches a registration nonce required for account registration.
	GetNonce(ctx context.Context) (*GetNonceResponse, error)

	// RegisterAccount registers a new account to Orderly (unique per wallet + builder).
	// https://orderly.network/docs/build-on-omnichain/evm-api/restful-api/public/register-account
	RegisterAccount(ctx context.Context, req RegisterAccountRequest) (*RegisterAccountResponse, error)

	// AddOrderlyKey registers an Orderly access key for an account.
	// https://orderly.network/docs/build-on-omnichain/evm-api/restful-api/public/add-orderly-key
	AddOrderlyKey(ctx context.Context, req AddOrderlyKeyRequest) (*AddOrderlyKeyResponse, error)
}

type Config struct {
	BaseURL string // https://api.orderly.org (mainnet) or https://testnet-api.orderly.org
	Timeout time.Duration
}

type client struct {
	http *resty.Client
}

func NewClient(cfg Config) Client {
	c := resty.New().
		SetBaseURL(cfg.BaseURL).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json")

	if cfg.Timeout > 0 {
		c.SetTimeout(cfg.Timeout)
	}

	return &client{http: c}
}

func (c *client) GetAccount(ctx context.Context, address, brokerID string) (*GetAccountResponse, error) {
	var out GetAccountResponse
	r, err := c.http.R().SetContext(ctx).
		SetQueryParam("address", address).
		SetQueryParam("broker_id", brokerID).
		SetQueryParam("chain_type", "SOL").
		SetResult(&out).
		Get("/v1/get_account")
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	if r.IsError() {
		return nil, fmt.Errorf("get account: %s %s", r.Status(), r.String())
	}
	return &out, nil
}

func (c *client) GetNonce(ctx context.Context) (*GetNonceResponse, error) {
	var out GetNonceResponse
	r, err := c.http.R().SetContext(ctx).
		SetResult(&out).
		Get("/v1/registration_nonce")
	if err != nil {
		return nil, fmt.Errorf("get nonce: %w", err)
	}
	if r.IsError() {
		return nil, fmt.Errorf("get nonce: %s %s", r.Status(), r.String())
	}
	if !out.Success {
		return nil, fmt.Errorf("get nonce: %s", out.Message)
	}
	return &out, nil
}

func (c *client) RegisterAccount(ctx context.Context, req RegisterAccountRequest) (*RegisterAccountResponse, error) {
	var out RegisterAccountResponse
	r, err := c.http.R().SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/register_account")
	if err != nil {
		return nil, fmt.Errorf("register account: %w", err)
	}
	if r.IsError() {
		return nil, fmt.Errorf("register account: %s %s", r.Status(), r.String())
	}
	if !out.Success {
		return nil, fmt.Errorf("register account: %s", out.Message)
	}
	return &out, nil
}

func (c *client) AddOrderlyKey(ctx context.Context, req AddOrderlyKeyRequest) (*AddOrderlyKeyResponse, error) {
	var out AddOrderlyKeyResponse
	r, err := c.http.R().SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/orderly_key")
	if err != nil {
		return nil, fmt.Errorf("add orderly key: %w", err)
	}
	if r.IsError() {
		return nil, fmt.Errorf("add orderly key: %s %s", r.Status(), r.String())
	}
	if !out.Success {
		return nil, fmt.Errorf("add orderly key: %s", out.Message)
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// PrivateClient — authenticated Orderly API (orders, positions, etc.)
// ---------------------------------------------------------------------------

type PrivateClient interface {
	CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResponse, error)
	CancelOrder(ctx context.Context, symbol string, orderID int) (*CancelOrderResponse, error)
	GetPositions(ctx context.Context) (*PositionsResponse, error)
	GetSettleNonce(ctx context.Context) (*SettleNonceResponse, error)
	PlaceAlgoOrder(ctx context.Context, req PlaceAlgoOrderRequest) (*PlaceAlgoOrderResponse, error)
	CancelAlgoOrder(ctx context.Context, symbol string, algoOrderID int) error
	GetAlgoOrders(ctx context.Context, symbol string) (*GetAlgoOrdersResponse, error)
}

type privateClient struct {
	http *resty.Client
}

func NewPrivateClient(baseURL, accountID, publicKey string, privateKey ed25519.PrivateKey) PrivateClient {
	c := resty.New().
		SetBaseURL(baseURL).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetTimeout(30 * time.Second)

	c.OnBeforeRequest(orderlySignMiddleware(accountID, publicKey, privateKey))

	return &privateClient{http: c}
}

func orderlySignMiddleware(accountID, publicKey string, privateKey ed25519.PrivateKey) resty.RequestMiddleware {
	return func(c *resty.Client, r *resty.Request) error {
		ts := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
		normalized := ts + r.Method + r.URL

		if len(r.QueryParam) != 0 {
			normalized += "?" + r.QueryParam.Encode()
		}

		if r.Method != http.MethodGet && r.Method != http.MethodDelete && r.Body != nil {
			bodyBytes, err := json.Marshal(r.Body)
			if err != nil {
				return fmt.Errorf("marshal body: %w", err)
			}
			normalized += string(bodyBytes)
		}

		sig := ed25519.Sign(privateKey, []byte(normalized))
		sigBase64 := base64.RawURLEncoding.EncodeToString(sig)

		r.SetHeader("orderly-account-id", accountID)
		r.SetHeader("orderly-key", "ed25519:"+publicKey)
		r.SetHeader("orderly-timestamp", ts)
		r.SetHeader("orderly-signature", sigBase64)

		if r.Method == http.MethodGet || r.Method == http.MethodDelete {
			r.SetHeader("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r.SetHeader("Content-Type", "application/json")
		}

		return nil
	}
}

func (c *privateClient) CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResponse, error) {
	var out CreateOrderResponse
	r, err := c.http.R().SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/order")
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	if r.IsError() || !out.Success {
		return nil, parseAPIError(r.Body(), "create order")
	}
	return &out, nil
}

func parseAPIError(body []byte, op string) error {
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Message != "" {
		return &APIError{Code: resp.Code, Message: resp.Message}
	}
	return fmt.Errorf("%s: %s", op, string(body))
}

func (c *privateClient) CancelOrder(ctx context.Context, symbol string, orderID int) (*CancelOrderResponse, error) {
	var out CancelOrderResponse
	r, err := c.http.R().SetContext(ctx).
		SetQueryParam("symbol", symbol).
		SetQueryParam("order_id", strconv.Itoa(orderID)).
		SetResult(&out).
		Delete("/v1/order")
	if err != nil {
		return nil, fmt.Errorf("cancel order: %w", err)
	}
	if r.IsError() {
		return nil, fmt.Errorf("cancel order: %s %s", r.Status(), r.String())
	}
	if !out.Success {
		return nil, fmt.Errorf("cancel order: %s", out.Message)
	}
	return &out, nil
}

func (c *privateClient) GetPositions(ctx context.Context) (*PositionsResponse, error) {
	var out PositionsResponse
	r, err := c.http.R().SetContext(ctx).
		SetResult(&out).
		Get("/v1/positions")
	if err != nil {
		return nil, fmt.Errorf("get positions: %w", err)
	}
	if r.IsError() {
		return nil, fmt.Errorf("get positions: %s %s", r.Status(), r.String())
	}
	if !out.Success {
		return nil, fmt.Errorf("get positions: %s", out.Message)
	}
	return &out, nil
}

func (c *privateClient) GetSettleNonce(ctx context.Context) (*SettleNonceResponse, error) {
	var out SettleNonceResponse
	r, err := c.http.R().SetContext(ctx).
		SetResult(&out).
		Get("/v1/settle_nonce")
	if err != nil {
		return nil, fmt.Errorf("get settle nonce: %w", err)
	}
	if r.IsError() {
		return nil, fmt.Errorf("get settle nonce: %s %s", r.Status(), r.String())
	}
	if !out.Success {
		return nil, fmt.Errorf("get settle nonce: %s", out.Message)
	}
	return &out, nil
}

func (c *privateClient) PlaceAlgoOrder(ctx context.Context, req PlaceAlgoOrderRequest) (*PlaceAlgoOrderResponse, error) {
	var out PlaceAlgoOrderResponse
	r, err := c.http.R().SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/algo/order")
	if err != nil {
		return nil, fmt.Errorf("place algo order: %w", err)
	}
	if r.IsError() || !out.Success {
		return nil, parseAPIError(r.Body(), "place algo order")
	}
	return &out, nil
}

func (c *privateClient) CancelAlgoOrder(ctx context.Context, symbol string, algoOrderID int) error {
	r, err := c.http.R().SetContext(ctx).
		SetQueryParam("symbol", symbol).
		SetQueryParam("algo_order_id", strconv.Itoa(algoOrderID)).
		Delete("/v1/algo/order")
	if err != nil {
		return fmt.Errorf("cancel algo order: %w", err)
	}
	if r.IsError() {
		return fmt.Errorf("cancel algo order: %s %s", r.Status(), r.String())
	}
	return nil
}

func (c *privateClient) GetAlgoOrders(ctx context.Context, symbol string) (*GetAlgoOrdersResponse, error) {
	var out GetAlgoOrdersResponse
	req := c.http.R().SetContext(ctx).SetResult(&out)
	if symbol != "" {
		req = req.SetQueryParam("symbol", symbol)
	}
	r, err := req.Get("/v1/algo/orders")
	if err != nil {
		return nil, fmt.Errorf("get algo orders: %w", err)
	}
	if r.IsError() {
		return nil, fmt.Errorf("get algo orders: %s %s", r.Status(), r.String())
	}
	if !out.Success {
		return nil, fmt.Errorf("get algo orders: %s", out.Message)
	}
	return &out, nil
}
