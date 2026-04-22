package recording

import (
	"context"
	"fmt"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/scrub"
	"stagehand/internal/scrub/detectors"
	"stagehand/internal/scrub/session_salt"
	"stagehand/internal/store"
)

type Writer struct {
	store           store.ArtifactStore
	saltManager     *session_salt.Manager
	scrubConfig     config.ScrubConfig
	rules           []scrub.Rule
	detectorLibrary *detectors.Library
}

type WriterOptions struct {
	Store       store.ArtifactStore
	SaltManager *session_salt.Manager
	ScrubConfig config.ScrubConfig
}

func NewWriter(opts WriterOptions) (*Writer, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("artifact store is required")
	}
	if opts.SaltManager == nil {
		return nil, fmt.Errorf("salt manager is required")
	}
	if !opts.ScrubConfig.Enabled {
		return nil, fmt.Errorf("scrub.enabled must be true for persisted recording")
	}

	rules, err := opts.ScrubConfig.Rules()
	if err != nil {
		return nil, fmt.Errorf("build scrub rules: %w", err)
	}

	return &Writer{
		store:       opts.Store,
		saltManager: opts.SaltManager,
		scrubConfig: opts.ScrubConfig,
		rules:       rules,
		detectorLibrary: detectors.LibraryForEnabled(detectors.Enabled{
			Email:      opts.ScrubConfig.Detectors.Email,
			JWT:        opts.ScrubConfig.Detectors.JWT,
			Phone:      opts.ScrubConfig.Detectors.Phone,
			SSN:        opts.ScrubConfig.Detectors.SSN,
			CreditCard: opts.ScrubConfig.Detectors.CreditCard,
			APIKey:     opts.ScrubConfig.Detectors.APIKey,
		}),
	}, nil
}

func (w *Writer) PersistInteraction(ctx context.Context, interaction recorder.Interaction) (recorder.Interaction, error) {
	run, err := w.store.GetRunRecord(ctx, interaction.RunID)
	if err != nil {
		return recorder.Interaction{}, fmt.Errorf("load run %q: %w", interaction.RunID, err)
	}
	if run.ScrubPolicyVersion != w.scrubConfig.PolicyVersion {
		return recorder.Interaction{}, fmt.Errorf(
			"run %q scrub_policy_version %q does not match writer policy %q",
			run.RunID,
			run.ScrubPolicyVersion,
			w.scrubConfig.PolicyVersion,
		)
	}

	salt, err := w.saltManager.GetOrCreate(ctx, run.SessionName)
	if err != nil {
		return recorder.Interaction{}, fmt.Errorf("get session salt for %q: %w", run.SessionName, err)
	}

	pipeline, err := scrub.NewPipeline(scrub.Options{
		PolicyVersion:   run.ScrubPolicyVersion,
		SessionSaltID:   salt.SaltID,
		HashSalt:        salt.Salt,
		Rules:           scrub.CloneRules(w.rules),
		DetectorLibrary: w.detectorLibrary,
	})
	if err != nil {
		return recorder.Interaction{}, fmt.Errorf("build scrub pipeline: %w", err)
	}

	scrubbed, err := pipeline.ScrubInteraction(interaction)
	if err != nil {
		return recorder.Interaction{}, fmt.Errorf("scrub interaction %q: %w", interaction.InteractionID, err)
	}

	if err := w.store.WriteInteraction(ctx, scrubbed); err != nil {
		return recorder.Interaction{}, fmt.Errorf("persist interaction %q: %w", interaction.InteractionID, err)
	}

	return scrubbed, nil
}
