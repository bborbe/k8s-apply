// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/iam"
	"github.com/bborbe/errors"

	"github.com/bborbe/k8s-apply/pkg/apply"
)

// InitiatorHeader is the header the command gateway reads the caller's identity
// from. The gateway's command handler resolves its initiator with
// req.Header.Get(login.LoginHeaderName), and login.LoginHeaderName is
// "X-Gateway-Initiator".
//
// It is a constant rather than a literal at the call site so the day the
// gateway renames the header the change is one line — and so the request-shape
// test can assert the literal and the constant separately.
//
// The header is not by itself an authentication mechanism. Measured against
// sun's live gateway on 2026-11-04: an unauthenticated POST to
// /api/1.0/command/k8s-apply-v1/apply/<id> is answered 307 to Google OAuth,
// with or without this header set. The gateway's own configuration shows why —
// it fronts a browser OAuth flow (GOOGLE_CLIENT_ID, GOOGLE_REDIRECT_URL) and
// separately carries a non-human identity named "api" in ALLOWED_USERS, backed
// by a username/password directory. So the caller sends both: HTTP Basic for
// the credential, and this header for the identity the gateway attributes the
// promotion to.
//
// One assumption remains unresolved and cannot be tested from here: whether the
// gateway's auth middleware overwrites X-Gateway-Initiator on a service-to-
// service call. If it does, the promotion is attributed to the wrong caller and
// nothing in this repository can detect it — the dev-side record only says the
// delivery was accepted.
const InitiatorHeader = "X-Gateway-Initiator"

// NewGateway returns the Gateway that carries a promote verdict to the sun
// command gateway.
//
// initiator is the identity the gateway attributes the promotion to; user and
// password are the credential it authenticates with. Both travel in request
// headers and nowhere else: never in the URL, never in the record, never in a
// log line or an error text.
func NewGateway(
	baseURL string,
	initiator iam.Initiator,
	user string,
	password string,
	client *http.Client,
) Gateway {
	return &gateway{
		baseURL:   strings.TrimRight(baseURL, "/"),
		initiator: initiator,
		user:      user,
		password:  password,
		client:    client,
	}
}

type gateway struct {
	baseURL   string
	initiator iam.Initiator
	user      string
	password  string
	client    *http.Client
}

// requestURL builds the gateway endpoint for one promotion. It is a named
// helper so the shape is assertable without standing up a server.
//
// The identifier is caller-supplied: the gateway parses the last path segment
// and carries it through as the command's own event identifier, which is what
// makes the promotion traceable on the sun side.
func requestURL(baseURL string, eventID base.EventID) string {
	return strings.TrimRight(baseURL, "/") +
		"/api/1.0/command/" + apply.SchemaID.String() +
		"/" + string(ApplyOperation) +
		"/" + url.PathEscape(eventID.String())
}

// Send issues exactly one apply command for componentPath to the sun gateway.
//
// There is no retry, no backoff and no queueing: a failed delivery is reported
// through the record and the dev result is published regardless. A promotion
// that could not be carried must never turn a successful dev apply into a
// failed one.
func (g *gateway) Send(
	ctx context.Context,
	componentPath string,
	eventID base.EventID,
) error {
	body, err := json.Marshal(apply.Command{Path: componentPath})
	if err != nil {
		return errors.Wrapf(ctx, err, "serialize promotion command for '%s' failed", componentPath)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		requestURL(g.baseURL, eventID),
		bytes.NewReader(body),
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "build promotion request for '%s' failed", componentPath)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(InitiatorHeader, g.initiator.String())
	// SetBasicAuth also sets the header, so a caller that configured no
	// credential sends none rather than an empty one.
	if g.user != "" {
		req.SetBasicAuth(g.user, g.password)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		// A transport failure, a timeout included, is a failed delivery.
		return errors.Wrapf(ctx, err, "promote '%s' to the sun gateway failed", componentPath)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		// The status is in the text because the operator needs to tell 429 from
		// 401; the initiator value is not, and never may be.
		return errors.Errorf(
			ctx,
			"promote '%s' to the sun gateway failed: gateway returned %d: %s",
			componentPath,
			resp.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}
	return nil
}
