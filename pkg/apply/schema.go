// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import "github.com/bborbe/cqrs/cdb"

// SchemaID identifies the apply command schema. Its string form
// "nuke-apply-v1" is the gateway path segment, and the branch prefix turns it
// into the dev topics develop-nuke-apply-v1-{request,result,event,history}.
var SchemaID = cdb.SchemaID{
	Group:   "nuke",
	Kind:    "apply",
	Version: "v1",
}
