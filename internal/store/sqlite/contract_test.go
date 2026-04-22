package sqlite

import (
	"testing"

	"stagehand/internal/store"
	"stagehand/internal/store/contracttest"
)

func TestArtifactStoreContract(t *testing.T) {
	contracttest.RunArtifactStoreTests(t, func(t *testing.T) store.ArtifactStore {
		t.Helper()
		return openTestStore(t)
	})
}
