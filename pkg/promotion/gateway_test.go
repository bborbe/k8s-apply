// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/iam"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/k8s-apply/pkg/promotion"
)

var _ = Describe("Gateway", func() {
	const (
		componentPath = "a2a-worker-controller"
		initiator     = "k8s-apply-dev"
		user          = "api"
		password      = "credential-must-not-leak"
		eventID       = base.EventID("11111111-1111-1111-1111-111111111111")
	)

	var ctx context.Context
	var server *httptest.Server
	var gateway promotion.Gateway

	var receivedMethod string
	var receivedPath string
	var receivedContentType string
	var receivedInitiator string
	var receivedUser string
	var receivedPassword string
	var receivedHasBasicAuth bool
	var receivedBody []byte
	var responseStatus int
	var responseBody string

	BeforeEach(func() {
		ctx = context.Background()
		receivedMethod = ""
		receivedPath = ""
		receivedContentType = ""
		receivedInitiator = ""
		receivedUser = ""
		receivedPassword = ""
		receivedHasBasicAuth = false
		receivedBody = nil
		responseStatus = http.StatusOK
		responseBody = ""

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			receivedPath = r.URL.Path
			receivedContentType = r.Header.Get("Content-Type")
			receivedInitiator = r.Header.Get("X-Gateway-Initiator")
			receivedUser, receivedPassword, receivedHasBasicAuth = r.BasicAuth()
			raw, err := io.ReadAll(r.Body)
			Expect(err).To(BeNil())
			receivedBody = raw
			w.WriteHeader(responseStatus)
			_, _ = w.Write([]byte(responseBody))
		}))
		DeferCleanup(server.Close)

		gateway = promotion.NewGateway(
			server.URL,
			iam.Initiator(initiator),
			user,
			password,
			&http.Client{Timeout: 10 * time.Second},
		)
	})

	It("carries the identifier in the path and only the component in the body", func() {
		Expect(gateway.Send(ctx, componentPath, eventID)).To(Succeed())

		Expect(receivedMethod).To(Equal(http.MethodPost))
		Expect(receivedPath).To(Equal("/api/1.0/command/k8s-apply-v1/apply/" + eventID.String()))
		Expect(receivedContentType).To(Equal("application/json"))

		// The literal header name, read with the literal — the constant is
		// asserted separately below, so a typo in either is caught.
		Expect(receivedInitiator).To(Equal(initiator))

		// The initiator header alone does not authenticate: sun's gateway answers
		// it with a 307 to Google OAuth. The Basic credential is what gets the
		// request through, so it is asserted on the wire, not merely configured.
		Expect(receivedHasBasicAuth).To(BeTrue())
		Expect(receivedUser).To(Equal(user))
		Expect(receivedPassword).To(Equal(password))

		// Decoding into a map and asserting the single key is what pins "the
		// promotion command carries only the component path".
		var decoded map[string]any
		Expect(json.Unmarshal(receivedBody, &decoded)).To(Succeed())
		Expect(decoded).To(HaveLen(1))
		Expect(decoded).To(HaveKeyWithValue("path", componentPath))
	})

	It("sends no credential when none is configured", func() {
		gateway = promotion.NewGateway(
			server.URL,
			iam.Initiator(initiator),
			"",
			"",
			&http.Client{Timeout: 10 * time.Second},
		)

		Expect(gateway.Send(ctx, componentPath, eventID)).To(Succeed())

		Expect(receivedHasBasicAuth).To(BeFalse())
		Expect(receivedInitiator).To(Equal(initiator))
	})

	It("names the initiator header the gateway actually reads", func() {
		Expect(promotion.InitiatorHeader).To(Equal("X-Gateway-Initiator"))
	})

	It("accepts a 200", func() {
		Expect(gateway.Send(ctx, componentPath, eventID)).To(Succeed())
	})

	It("reports a 500 by status and never by initiator", func() {
		responseStatus = http.StatusInternalServerError
		responseBody = "boom"

		err := gateway.Send(ctx, componentPath, eventID)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("500"))
		Expect(err.Error()).NotTo(ContainSubstring(initiator))
		Expect(err.Error()).NotTo(ContainSubstring(password))
	})

	It("reports a 401 by status and never by initiator", func() {
		responseStatus = http.StatusUnauthorized

		err := gateway.Send(ctx, componentPath, eventID)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("401"))
		Expect(err.Error()).NotTo(ContainSubstring(initiator))
		Expect(err.Error()).NotTo(ContainSubstring(password))
	})

	It("reports an unreachable gateway by component", func() {
		server.Close()

		err := gateway.Send(ctx, componentPath, eventID)

		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring(componentPath))
		Expect(err.Error()).NotTo(ContainSubstring(initiator))
		Expect(err.Error()).NotTo(ContainSubstring(password))
	})
})
