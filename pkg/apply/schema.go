// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import "github.com/bborbe/cqrs/cdb"

// SchemaID identifies the apply command schema. Its string form
// "maintainer-kubeapply-v1" is the gateway path segment.
//
// The Seibert stages consume this schema on the UNPREFIXED topics
// maintainer-kubeapply-v1-{request,result,event,history}, which is what
// data/cdb-config's topics/maintainer.yaml declares — see TopicPrefix below.
var SchemaID = cdb.SchemaID{
	Group:   "maintainer",
	Kind:    "kubeapply",
	Version: "v1",
}
