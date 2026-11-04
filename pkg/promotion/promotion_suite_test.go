// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package promotion_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate

func TestPromotion(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Promotion Suite")
}
