package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// bootstrapOAuth creates and authorizes a real device grant.
//
// The OAuth device grant store lives in the API process (deviceGrantStore), so
// the seed process cannot write it. Instead the harness drives the public
// endpoints exactly as a client would: register the seeded application, request
// a device code with its client_id, then authorize the returned user_code as
// the logged-in viewer. Every later OAuth case uses the resulting device code,
// so it measures the handler rather than "invalid client_id".
func (c *client) bootstrapOAuth() error {
	if c.fx.AppClientID == "" {
		return fmt.Errorf("fixtures carry no oauth client id (re-run -seed)")
	}
	codeBody, _ := json.Marshal(map[string]string{"client_id": c.fx.AppClientID})
	var codeResp struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	if err := c.postJSON("/oauth/device/code", codeBody, &codeResp); err != nil {
		return fmt.Errorf("device/code: %w", err)
	}
	if codeResp.DeviceCode == "" || codeResp.UserCode == "" {
		return fmt.Errorf("device/code returned no codes")
	}

	verifyBody, _ := json.Marshal(map[string]string{"user_code": codeResp.UserCode})
	resp, err := c.doJSON("POST", "/oauth/device/verify", verifyBody, true)
	if err != nil {
		return fmt.Errorf("device/verify: %w", err)
	}
	if resp.status != 200 {
		return fmt.Errorf("device/verify: status %d: %s", resp.status, truncate(string(resp.raw), 200))
	}
	c.fx.DeviceCode = codeResp.DeviceCode
	return nil
}

type jsonResponse struct {
	status int
	raw    []byte
}

func (c *client) doJSON(method, path string, body []byte, auth bool) (jsonResponse, error) {
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return jsonResponse{}, err
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return jsonResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return jsonResponse{status: resp.StatusCode, raw: raw}, nil
}

func (c *client) postJSON(path string, body []byte, out any) error {
	resp, err := c.doJSON("POST", path, body, false)
	if err != nil {
		return err
	}
	if resp.status != 200 {
		return fmt.Errorf("status %d: %s", resp.status, truncate(string(resp.raw), 200))
	}
	return json.Unmarshal(resp.raw, out)
}
