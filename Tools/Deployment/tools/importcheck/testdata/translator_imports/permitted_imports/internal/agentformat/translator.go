package agentformat

// translator.go is a test fixture representing a translator-layer file that uses
// only the permitted import set. The translator-layer purity and direction rules
// must ACCEPT this file: it imports only packages that the translator layer is
// legitimately allowed to use.
//
// The permitted set includes:
//   - internal/domain      (base vocabulary, no upward dependency)
//   - internal/agentfields (leaf key-vocabulary, no imports of its own; on the
//                           permitted list deliberately so the translator need not
//                           duplicate the MOSAIC-owned key sets)
//   - mosaic-common/docformat (the document model the translator produces)
//   - mosaic-common/mosaic    (the field-value type the translator uses)
//   - the TOML library and the standard library minus the banned set
//
// This fixture exists to prove the acceptance case: the translator-layer rule does
// not degrade into rejecting every import, only the forbidden ones.

import (
	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/domain"
	"mosaic-common/docformat"
)

// agentKey is a placeholder that exercises the permitted import set.
// This file is a fixture and is never compiled against the real module.
var _ = agentfields.All
var _ = domain.ArtifactKindAgent
var _ = docformat.Parse
